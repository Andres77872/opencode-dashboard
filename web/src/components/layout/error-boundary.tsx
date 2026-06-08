/* View-level error boundary — a single view crashing shows a recoverable card
   instead of blanking the whole app. Keyed by route so navigation clears it. */
import { Component, type ErrorInfo, type ReactNode } from 'react'
import { ErrorState } from '../vael'

interface Props {
  children: ReactNode
}
interface State {
  error: Error | null
}

export class ViewErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('View error boundary caught:', error, info)
  }

  render() {
    if (this.state.error) {
      return (
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)' }}>
          <ErrorState
            title="This view hit an error"
            message={this.state.error.message}
            onRetry={() => window.location.reload()}
          />
        </div>
      )
    }
    return this.props.children
  }
}
