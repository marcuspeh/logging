import { useMemo } from "react";
import { cn } from "../../lib/utils";

interface JsonHighlighterProps {
  code: string;
  className?: string;
}

// JsonHighlighter pretty-prints a JSON string and colour-codes keys,
// string values, numbers, booleans and nulls the same way the
// prototype's dark payload panel does.
//
// Falls back to plain rendering when the input isn't valid JSON.
export function JsonHighlighter({ code, className }: JsonHighlighterProps) {
  const { formattedCode, isJson } = useMemo(() => {
    try {
      const parsed = JSON.parse(code);
      return {
        formattedCode: JSON.stringify(parsed, null, 2),
        isJson: true,
      };
    } catch {
      return { formattedCode: code, isJson: false };
    }
  }, [code]);

  if (!isJson) {
    return (
      <pre className={cn("font-mono leading-relaxed whitespace-pre-wrap break-words", className)}>
        {formattedCode}
      </pre>
    );
  }

  const renderJson = () => {
    const jsonRegex =
      /("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?)/g;
    let lastIndex = 0;
    const elements: React.ReactNode[] = [];
    let match: RegExpExecArray | null;

    while ((match = jsonRegex.exec(formattedCode)) !== null) {
      const matchString = match[0];
      const matchIndex = match.index;

      if (matchIndex > lastIndex) {
        elements.push(
          <span key={`text-${lastIndex}`} className="text-slate-400">
            {formattedCode.substring(lastIndex, matchIndex)}
          </span>,
        );
      }

      if (/^"/.test(matchString)) {
        if (/:$/.test(matchString)) {
          const colonIndex = matchString.lastIndexOf(":");
          const keyString = matchString.substring(0, colonIndex);
          const trailing = matchString.substring(colonIndex);

          elements.push(
            <span key={`match-${matchIndex}-key`} className="text-blue-400 font-medium">
              {keyString}
            </span>,
          );
          elements.push(
            <span key={`match-${matchIndex}-colon`} className="text-slate-400">
              {trailing}
            </span>,
          );
        } else {
          elements.push(
            <span key={`match-${matchIndex}`} className="text-emerald-400">
              {matchString}
            </span>,
          );
        }
      } else if (/true|false/.test(matchString)) {
        elements.push(
          <span key={`match-${matchIndex}`} className="text-purple-400 font-medium">
            {matchString}
          </span>,
        );
      } else if (/null/.test(matchString)) {
        elements.push(
          <span key={`match-${matchIndex}`} className="text-purple-400 font-medium italic">
            {matchString}
          </span>,
        );
      } else {
        elements.push(
          <span key={`match-${matchIndex}`} className="text-orange-400">
            {matchString}
          </span>,
        );
      }

      lastIndex = jsonRegex.lastIndex;
    }

    if (lastIndex < formattedCode.length) {
      elements.push(
        <span key={`text-${lastIndex}`} className="text-slate-400">
          {formattedCode.substring(lastIndex)}
        </span>,
      );
    }

    return elements;
  };

  return (
    <pre className={cn("font-mono leading-relaxed whitespace-pre-wrap break-words", className)}>
      {renderJson()}
    </pre>
  );
}