import { useQuery } from "@tanstack/react-query";
import { getProjects } from "../api/logs";

// useProjects fetches the deduplicated, sorted project list once per
// mount. The result drives a <datalist> autocomplete for the project
// filter. The input itself stays a free-text input so users can still
// type custom project names that aren't yet in the index.
export function useProjects() {
  return useQuery({
    queryKey: ["projects"],
    queryFn: getProjects,
    staleTime: 60_000,
    retry: 1,
  });
}