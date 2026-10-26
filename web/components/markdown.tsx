import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

// The agent's answers: GitHub-flavoured Markdown (tables, lists, bold, links).
export function Markdown({ text }: { text: string }) {
  return (
    <div className="md text-sm leading-relaxed">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{ a: (props) => <a {...props} target="_blank" rel="noopener noreferrer" /> }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}
