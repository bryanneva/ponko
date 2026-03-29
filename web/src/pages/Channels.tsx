import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import styles from './Channels.module.css'

interface Channel {
  channelId: string
  channelName: string
  systemPrompt: string
  respondMode: string
  toolAllowlist: string[] | null
  createdAt: string
  updatedAt: string
}

function truncate(text: string, max: number): string {
  if (text.length <= max) return text
  return text.slice(0, max) + '...'
}

export default function Channels() {
  const [channels, setChannels] = useState<Channel[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const navigate = useNavigate()

  useEffect(() => {
    fetch('/api/channels')
      .then(res => res.json())
      .then(data => {
        setChannels(data)
        setLoading(false)
      })
      .catch(() => {
        setError(true)
        setLoading(false)
      })
  }, [])

  if (loading) {
    return <div className={styles.loading}>Loading channels...</div>
  }

  if (error) {
    return (
      <div>
        <div className={styles.header}>
          <h1 className={styles.title}>Channels</h1>
        </div>
        <div className={styles.empty}>Failed to load channels.</div>
      </div>
    )
  }

  if (channels.length === 0) {
    return (
      <div>
        <div className={styles.header}>
          <h1 className={styles.title}>Channels</h1>
        </div>
        <div className={styles.empty}>No channels configured yet.</div>
      </div>
    )
  }

  return (
    <div>
      <div className={styles.header}>
        <h1 className={styles.title}>Channels</h1>
      </div>
      <table className={styles.table}>
        <thead>
          <tr>
            <th>Channel</th>
            <th>Respond Mode</th>
            <th>Tools</th>
            <th>System Prompt</th>
          </tr>
        </thead>
        <tbody>
          {channels.map(ch => (
            <tr
              key={ch.channelId}
              className={styles.clickableRow}
              onClick={() => navigate(`/channels/${ch.channelId}`)}
            >
              <td>{ch.channelName ? `#${ch.channelName}` : <code>{ch.channelId}</code>}</td>
              <td>{ch.respondMode || 'mention_only'}</td>
              <td>{ch.toolAllowlist === null ? 'All' : ch.toolAllowlist.length}</td>
              <td className={styles.prompt}>
                {ch.systemPrompt ? truncate(ch.systemPrompt, 80) : '—'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
