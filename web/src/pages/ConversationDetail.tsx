import { useCallback, useEffect, useRef, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import styles from './ConversationDetail.module.css'

interface TurnSummary {
  turn_id: string
  workflow_id: string
  trigger_type: string
  turn_index: number
  created_at: string
}

interface OutboxEntry {
  outbox_id: string
  status: string
  message_type: string
  attempts: number
  delivered_at: string | null
  created_at: string
}

interface ConversationData {
  conversation_id: string
  channel_id: string
  status: string
  created_at: string
  updated_at: string
  turns: TurnSummary[]
  outbox_entries: OutboxEntry[]
}

const statusClass: Record<string, string> = {
  active: styles.statusActive,
  awaiting_approval: styles.statusAwaitingApproval,
  completed: styles.statusCompleted,
  failed: styles.statusFailed,
}

const outboxStatusClass: Record<string, string> = {
  pending: styles.outboxPending,
  claimed: styles.outboxClaimed,
  delivered: styles.outboxDelivered,
  failed: styles.outboxFailed,
}

function formatTime(iso: string | null): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString()
}

function isTerminal(status: string): boolean {
  return status === 'completed' || status === 'failed'
}

export default function ConversationDetail() {
  const { id } = useParams<{ id: string }>()
  const [conversation, setConversation] = useState<ConversationData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const intervalRef = useRef<number | null>(null)

  const fetchData = useCallback(() => {
    if (!id) return
    fetch(`/api/conversations/${id}`)
      .then(r => {
        if (!r.ok) {
          if (r.status === 404) throw new Error('Conversation not found')
          throw new Error('Failed to load conversation')
        }
        return r.json()
      })
      .then((data: ConversationData) => {
        setConversation(data)
        setLoading(false)
        if (isTerminal(data.status) && intervalRef.current !== null) {
          window.clearInterval(intervalRef.current)
          intervalRef.current = null
        }
      })
      .catch(err => {
        setError(err.message)
        setLoading(false)
        if (intervalRef.current !== null) {
          window.clearInterval(intervalRef.current)
          intervalRef.current = null
        }
      })
  }, [id])

  useEffect(() => {
    fetchData()
    intervalRef.current = window.setInterval(fetchData, 5000)
    return () => {
      if (intervalRef.current !== null) window.clearInterval(intervalRef.current)
    }
  }, [fetchData])

  if (loading) {
    return <div className={styles.loading}>Loading conversation...</div>
  }

  if (error || !conversation) {
    return (
      <div>
        <Link to="/conversations" className={styles.backLink}>Back to Conversations</Link>
        <div className={styles.loading}>{error || 'Conversation not found'}</div>
      </div>
    )
  }

  return (
    <div>
      <Link to="/conversations" className={styles.backLink}>Back to Conversations</Link>

      <div className={styles.headerRow}>
        <h1 className={styles.title}>Conversation</h1>
        <span className={`${styles.statusBadge} ${statusClass[conversation.status] || ''}`}>
          {conversation.status.replace('_', ' ')}
        </span>
      </div>

      <div className={styles.meta}>
        <div>Channel: {conversation.channel_id}</div>
        <div>Created: {formatTime(conversation.created_at)}</div>
        <div>Updated: {formatTime(conversation.updated_at)}</div>
      </div>

      <h2 className={styles.sectionTitle}>Turns</h2>
      {conversation.turns.length === 0 ? (
        <div className={styles.empty}>No turns yet.</div>
      ) : (
        <table className={styles.table}>
          <thead>
            <tr>
              <th>#</th>
              <th>Trigger</th>
              <th>Workflow</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {conversation.turns.map(t => (
              <tr key={t.turn_id}>
                <td>{t.turn_index}</td>
                <td>{t.trigger_type}</td>
                <td>
                  <Link to={`/workflows/${t.workflow_id}`} className={styles.link}>
                    {t.workflow_id.slice(0, 8)}...
                  </Link>
                </td>
                <td>{formatTime(t.created_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h2 className={styles.sectionTitle}>Outbox Entries</h2>
      {conversation.outbox_entries.length === 0 ? (
        <div className={styles.empty}>No outbox entries.</div>
      ) : (
        <table className={styles.table}>
          <thead>
            <tr>
              <th>Status</th>
              <th>Type</th>
              <th>Attempts</th>
              <th>Delivered</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {conversation.outbox_entries.map(e => (
              <tr key={e.outbox_id} className={e.status === 'failed' ? styles.failedRow : undefined}>
                <td>
                  <span className={`${styles.outboxBadge} ${outboxStatusClass[e.status] || ''}`}>
                    {e.status}
                  </span>
                </td>
                <td>{e.message_type}</td>
                <td>{e.attempts}</td>
                <td>{formatTime(e.delivered_at)}</td>
                <td>{formatTime(e.created_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
