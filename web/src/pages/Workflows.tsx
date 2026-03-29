import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import styles from './Workflows.module.css'

interface WorkflowSummary {
  workflow_id: string
  workflow_type: string
  status: string
  created_at: string
  updated_at: string
  total_tasks: number
  completed_tasks: number
}

const statusClass: Record<string, string> = {
  pending: styles.statusPending,
  running: styles.statusRunning,
  completed: styles.statusCompleted,
  complete: styles.statusCompleted,
  failed: styles.statusFailed,
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString()
}

function progressText(wf: WorkflowSummary): string {
  if (wf.total_tasks === 0) return '—'
  return `${wf.completed_tasks}/${wf.total_tasks}`
}

export default function Workflows() {
  const [workflows, setWorkflows] = useState<WorkflowSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const intervalRef = useRef<number | null>(null)

  const fetchData = useCallback(() => {
    fetch('/api/workflows/recent?limit=50')
      .then(r => {
        if (!r.ok) throw new Error('Failed to load workflows')
        return r.json()
      })
      .then((data: WorkflowSummary[]) => {
        setWorkflows(data ?? [])
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
    return <div className={styles.loading}>Loading workflows...</div>
  }

  if (error) {
    return (
      <div>
        <div className={styles.header}>
          <h1 className={styles.title}>Workflows</h1>
          <button className={styles.refreshBtn} onClick={fetchData}>Refresh</button>
        </div>
        <div className={styles.loading}>Failed to load workflows.</div>
      </div>
    )
  }

  return (
    <div>
      <div className={styles.header}>
        <h1 className={styles.title}>Workflows</h1>
        <button className={styles.refreshBtn} onClick={fetchData}>Refresh</button>
      </div>

      <table className={styles.table}>
        <thead>
          <tr>
            <th>Type</th>
            <th>Status</th>
            <th>Progress</th>
            <th>Created</th>
          </tr>
        </thead>
        <tbody>
          {workflows.length === 0 ? (
            <tr>
              <td colSpan={4} className={styles.empty}>No workflows yet.</td>
            </tr>
          ) : (
            workflows.map(wf => (
              <tr key={wf.workflow_id}>
                <td>
                  <Link to={`/workflows/${wf.workflow_id}`} className={styles.link}>
                    {wf.workflow_type}
                  </Link>
                </td>
                <td>
                  <span className={`${styles.statusBadge} ${statusClass[wf.status] || ''}`}>
                    {wf.status}
                  </span>
                </td>
                <td>{progressText(wf)}</td>
                <td>{formatTime(wf.created_at)}</td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  )
}
