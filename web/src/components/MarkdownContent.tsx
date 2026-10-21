import ReactMarkdown from 'react-markdown'
import remarkBreaks from 'remark-breaks'

export function MarkdownContent({ value, className = '' }: { value: string; className?: string }) {
  return <div className={`mcp-markdown ${className}`}>
    <ReactMarkdown skipHtml remarkPlugins={[remarkBreaks]} components={{
      a: ({ href, children }) => <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>,
    }}>{value}</ReactMarkdown>
  </div>
}
