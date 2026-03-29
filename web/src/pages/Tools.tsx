import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import styles from './Tools.module.css'

interface Channel {
  channelId: string
  channelName: string
  toolAllowlist: string[] | null
}

interface ToolRow {
  name: string
  channels: { id: string; label: string; allowed: 'all' | 'explicit' }[]
}

export default function Tools() {
  const [rows, setRows] = useState<ToolRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  useEffect(() => {
    Promise.all([
      fetch('/api/tools').then(r => r.json()),
      fetch('/api/channels').then(r => r.json()),
    ])
      .then(([toolsData, channels]: [{ tools: string[] }, Channel[]]) => {
        const toolRows: ToolRow[] = toolsData.tools.map(name => {
          const chans = channels
            .filter(ch => ch.toolAllowlist === null || ch.toolAllowlist.includes(name))
            .map(ch => ({
              id: ch.channelId,
              label: ch.channelName ? `#${ch.channelName}` : ch.channelId,
              allowed: (ch.toolAllowlist === null ? 'all' : 'explicit') as 'all' | 'explicit',
            }))
          return { name, channels: chans }
        })
        setRows(toolRows)
        setLoading(false)
      })
      .catch(() => {
        setError(true)
        setLoading(false)
      })
  }, [])

  if (loading) {
    return <div className={styles.loading}>Loading tools...</div>
  }

  if (error) {
    return (
      <div>
        <div className={styles.header}>
          <h1 className={styles.title}>Tools</h1>
        </div>
        <div className={styles.empty}>Failed to load tools.</div>
      </div>
    )
  }

  if (rows.length === 0) {
    return (
      <div>
        <div className={styles.header}>
          <h1 className={styles.title}>Tools</h1>
        </div>
        <div className={styles.empty}>No tools available.</div>
      </div>
    )
  }

  return (
    <div>
      <div className={styles.header}>
        <h1 className={styles.title}>Tools</h1>
      </div>
      <table className={styles.table}>
        <thead>
          <tr>
            <th>Tool</th>
            <th>Allowed In Channels</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(row => (
            <tr key={row.name}>
              <td><code>{row.name}</code></td>
              <td>
                {row.channels.length === 0 ? (
                  '—'
                ) : (
                  row.channels.map(ch => (
                    <span key={ch.id}>
                      <Link className={styles.channelLink} to={`/channels/${ch.id}`}>
                        {ch.label}
                      </Link>
                      {ch.allowed === 'all' && (
                        <span className={styles.allBadge}>(all)</span>
                      )}
                    </span>
                  ))
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
