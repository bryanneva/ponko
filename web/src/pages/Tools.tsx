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
    const fetchTools = fetch('/api/tools').then(r => {
      if (!r.ok) throw new Error(`Tools endpoint returned ${r.status}`)
      return r.json()
    })
    const fetchChannels = fetch('/api/channels')
      .then(r => {
        if (!r.ok) throw new Error(`Channels endpoint returned ${r.status}`)
        return r.json()
      })
      .catch((err) => {
        console.error('Failed to fetch channels:', err)
        return [] as Channel[]
      })

    fetchTools
      .then((toolsData: { tools: string[] }) => {
        return fetchChannels.then((channels: Channel[]) => {
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
        <div className={styles.empty}>Could not load tools. Check that the server is running and you are logged in.</div>
      </div>
    )
  }

  if (rows.length === 0) {
    return (
      <div>
        <div className={styles.header}>
          <h1 className={styles.title}>Tools</h1>
        </div>
        <div className={styles.empty}>No tools available. Connect an MCP server to add tools.</div>
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
