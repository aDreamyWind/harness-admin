interface StatusIndicatorProps {
  label: string
  tone?: 'success' | 'warning' | 'danger' | 'muted' | 'info'
}

export function StatusIndicator({ label, tone = 'muted' }: StatusIndicatorProps) {
  return (
    <span className={`status-indicator status-indicator-${tone}`}>
      <span className="status-indicator-dot" aria-hidden="true" />
      {label}
    </span>
  )
}
