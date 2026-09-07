package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/marcuspeh/logging-backend/internal/model"
)

// Command smoke produces 1000 synthetic log events into the `logs` Kafka
// topic, waits for the collector to drain them, and verifies that the HTTP
// /query API returns the expected counts per project and per logid, and
// that Parquet files have been written (visible via /files).
//
// Run from backend/:  go run ./cmd/smoke
func main() {
	var (
		brokers = flag.String("brokers", "localhost:9092", "Kafka bootstrap address")
		apiURL  = flag.String("api", "http://localhost:4665", "Collector HTTP base URL")
		topic   = flag.String("topic", "logs", "Kafka topic")
		nEvents = flag.Int("events", 1000, "Number of events to produce")
		timeout = flag.Duration("timeout", 60*time.Second, "How long to wait for the collector to drain")
	)
	flag.Parse()

	if err := run([]string{*brokers}, *apiURL, *topic, *nEvents, *timeout); err != nil {
		log.Fatalf("smoke: %v", err)
	}
}

func run(brokers []string, apiURL, topic string, nEvents int, timeout time.Duration) error {
	projects := []string{"billing-service", "auth-service", "reports-service"}
	logids := []string{"req-7f2c", "req-9a31", "req-bd04", "req-c7e2", "req-557a"}

	log.Printf("producing %d events to %s on %v", nEvents, topic, brokers)
	if err := produce(brokers, topic, nEvents, projects, logids); err != nil {
		return fmt.Errorf("produce: %w", err)
	}

	log.Printf("waiting up to %s for the collector to write Parquet files", timeout)
	if err := waitForFiles(apiURL, timeout); err != nil {
		return fmt.Errorf("wait for files: %w", err)
	}

	expectedPerProject := nEvents / len(projects)
	totalExpected := expectedPerProject * len(projects)

	for _, p := range projects {
		got, err := queryCount(apiURL, map[string]string{"project": p})
		if err != nil {
			return fmt.Errorf("query project=%s: %w", p, err)
		}
		if got < expectedPerProject {
			return fmt.Errorf("project %s: got %d rows, want >= %d", p, got, expectedPerProject)
		}
		log.Printf("OK project=%s count=%d (>= %d)", p, got, expectedPerProject)
	}

	firstLogID := logids[0]
	got, err := queryCount(apiURL, map[string]string{"logid": firstLogID})
	if err != nil {
		return fmt.Errorf("query logid=%s: %w", firstLogID, err)
	}
	expectedPerLogID := nEvents / len(logids)
	if got < expectedPerLogID {
		return fmt.Errorf("logid %s: got %d rows, want >= %d", firstLogID, got, expectedPerLogID)
	}
	log.Printf("OK logid=%s count=%d (>= %d)", firstLogID, got, expectedPerLogID)

	total, err := queryCount(apiURL, map[string]string{})
	if err != nil {
		return fmt.Errorf("total query: %w", err)
	}
	if total < totalExpected {
		return fmt.Errorf("total: got %d, want >= %d", total, totalExpected)
	}
	log.Printf("OK total across all queries=%d (>= %d)", total, totalExpected)

	log.Printf("SMOKE OK")
	return nil
}

func produce(brokers []string, topic string, n int, projects, logids []string) error {
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireAll,
		BatchTimeout: 50 * time.Millisecond,
	}
	defer w.Close()

	msgs := make([]kafka.Message, 0, n)
	base := time.Now().UTC()
	for i := 0; i < n; i++ {
		p := projects[i%len(projects)]
		lid := logids[i%len(logids)]
		ev := model.LogEvent{
			Timestamp: base.Add(time.Duration(i) * time.Millisecond),
			Project:   p,
			LogID:     lid,
			Level:     levelFor(i),
			Message:   fmt.Sprintf("event %d for %s", i, p),
		}
		body, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		msgs = append(msgs, kafka.Message{
			Key:   []byte(p),
			Value: body,
		})
	}
	return w.WriteMessages(context.Background(), msgs...)
}

func levelFor(i int) string {
	switch i % 5 {
	case 0:
		return "DEBUG"
	case 1:
		return "INFO"
	case 2:
		return "WARN"
	case 3:
		return "ERROR"
	default:
		return "INFO"
	}
}

func waitForFiles(apiURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := apiURL + "/files"
	for time.Now().Before(deadline) {
		n, err := countFiles(url)
		if err != nil {
			return err
		}
		if n > 0 {
			log.Printf("found %d Parquet file(s)", n)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("no Parquet files appeared within %s", timeout)
}

func countFiles(url string) (int, error) {
	resp, err := http.Get(url)
	if err != nil {
		if isTransientNet(err) {
			return 0, nil
		}
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	var files []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return 0, err
	}
	return len(files), nil
}

func queryCount(apiURL string, params map[string]string) (int, error) {
	u := buildQueryURL(apiURL, params)

	resp, err := http.Get(u)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("GET %s: status %d body=%s", u, resp.StatusCode, body)
	}
	var got struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		return 0, fmt.Errorf("decode %s: %w (body=%s)", u, err, body)
	}
	return got.Count, nil
}

func buildQueryURL(apiURL string, params map[string]string) string {
	base, err := url.Parse(apiURL + "/query")
	if err != nil {
		return apiURL + "/query?project=billing-service"
	}
	q := base.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	if q.Get("project") == "" && q.Get("logid") == "" {
		q.Set("project", "billing-service")
	}
	base.RawQuery = q.Encode()
	return base.String()
}

func isTransientNet(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "EOF")
}
