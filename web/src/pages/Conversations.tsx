import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import styles from './Conversations.module.css'

interface ConversationSummary {
  conversation_id: string
  channel_id: string
  status: string
  turn_count: number
  created_at: string
  updated_at: string
}

const statusClass: Record<string, string> = {
  active: styles.statusActive,
  awaiting_approval: styles.statusAwaitingApproval,
  completed: styles.statusCompleted,
  failed: styles.statusFailed,
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString()
}

export default function Conversations() {
  const [conversations, setConversations] = useState<ConversationSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const intervalRef = useRef<number | null>(null)

  const fetchData = useCallback(() => {
    fetch('/api/conversations/recent?limit=50')
      .then(r => {
        if (!r.ok) throw new Error('Failed to load conversations')
        return r.json()
      })
      .then((data: ConversationSummary[]) => {
        setConversations(data ?? [])
        setLoading(false)
      })
      .catch(() => {
        setError(true)
        setLoading(false)
      })
  }, [])

  useEffect(() => {
    fetchData()
    intervalRef.current = window.setInterval(fetchData, 30000)
    return () => {
      if (intervalRef.current !== null) window.clearInterval(intervalRef.current)
    }
  }, [fetchData])

  if (loading) {
    return <div className={styles.loading}>Loading conversations...</div>
  }

  if (error) {
    return (
      <div>
        <div className={styles.header}>
          <h1 className={styles.title}>Conversations</h1>
          <button className={styles.refreshBtn} onClick={fetchData}>Refresh</button>
        </div>
        <div className={styles.loading}>Failed to load conversations.</div>
      </div>
    )
  }

  return (
    <div>
      <div className={styles.header}>
        <h1 className={styles.title}>Conversations</h1>
        <button className={styles.refreshBtn} onClick={fetchData}>Refresh</button>
      </div>

      <table className={styles.table}>
        <thead>
          <tr>
            <th>Status</th>
            <th>Channel</th>
            <th>Turns</th>
            <th>Created</th>
            <th>Updated</th>
          </tr>
        </thead>
        <tbody>
          {conversations.length === 0 ? (
            <tr>
              <td colSpan={5} className={styles.empty}>No conversations yet.</td>
            </tr>
          ) : (
            conversations.map(c => (
              <tr key={c.conversation_id}>
                <td>
                  <Link to={`/conversations/${c.conversation_id}`} className={styles.link}>
                    <span className={`${styles.statusBadge} ${statusClass[c.status] || ''}`}>
                      {c.status.replace('_', ' ')}
                    </span>
                  </Link>
                </td>
                <td>{c.channel_id}</td>
                <td>{c.turn_count}</td>
                <td>{formatTime(c.created_at)}</td>
                <td>{formatTime(c.updated_at)}</td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  )
}
