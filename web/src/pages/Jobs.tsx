import { useCallback, useEffect, useRef, useState } from 'react'
import styles from './Jobs.module.css'

interface JobSummary {
  available: number
  running: number
  completed: number
  discarded: number
  cancelled: number
  scheduled: number
  retryable: number
}

interface RecentJob {
  id: number
  kind: string
  state: string
  createdAt: string
  finalizedAt: string | null
  errors: string[]
}

const stateClass: Record<string, string> = {
  available: styles.stateAvailable,
  running: styles.stateRunning,
  completed: styles.stateCompleted,
  discarded: styles.stateDiscarded,
  cancelled: styles.stateCancelled,
  retryable: styles.stateRetryable,
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString()
}

export default function Jobs() {
  const [summary, setSummary] = useState<JobSummary | null>(null)
  const [jobs, setJobs] = useState<RecentJob[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const intervalRef = useRef<number | null>(null)

  const fetchData = useCallback(() => {
    Promise.all([
      fetch('/api/jobs/summary').then(r => r.json()),
      fetch('/api/jobs/recent?limit=50').then(r => r.json()),
    ])
      .then(([s, j]) => {
        setSummary(s)
        setJobs(j)
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
    return <div className={styles.loading}>Loading jobs...</div>
  }

  if (error) {
    return (
      <div>
        <div className={styles.header}>
          <h1 className={styles.title}>Jobs</h1>
          <button className={styles.refreshBtn} onClick={fetchData}>Refresh</button>
        </div>
        <div className={styles.loading}>Failed to load jobs.</div>
      </div>
    )
  }

  const summaryCards = summary
    ? [
        { label: 'Available', count: summary.available },
        { label: 'Running', count: summary.running },
        { label: 'Completed', count: summary.completed },
        { label: 'Discarded', count: summary.discarded },
        { label: 'Cancelled', count: summary.cancelled },
        { label: 'Scheduled', count: summary.scheduled },
        { label: 'Retryable', count: summary.retryable },
      ]
    : []

  return (
    <div>
      <div className={styles.header}>
        <h1 className={styles.title}>Jobs</h1>
        <button className={styles.refreshBtn} onClick={fetchData}>
          Refresh
        </button>
      </div>

      {summary && (
        <div className={styles.cards}>
          {summaryCards.map(c => (
            <div key={c.label} className={styles.card}>
              <div className={styles.cardLabel}>{c.label}</div>
              <div className={styles.cardCount}>{c.count}</div>
            </div>
          ))}
        </div>
      )}

      <table className={styles.table}>
        <thead>
          <tr>
            <th>Kind</th>
            <th>State</th>
            <th>Created</th>
            <th>Completed</th>
            <th>Error</th>
          </tr>
        </thead>
        <tbody>
          {jobs.map(job => (
            <tr key={job.id}>
              <td><code>{job.kind}</code></td>
              <td className={stateClass[job.state] || styles.stateDefault}>
                {job.state}
              </td>
              <td>{formatTime(job.createdAt)}</td>
              <td>{job.finalizedAt ? formatTime(job.finalizedAt) : '—'}</td>
              <td className={styles.error}>
                {job.errors.length > 0 ? job.errors[job.errors.length - 1] : '—'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
