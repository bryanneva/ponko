import { useCallback, useEffect, useRef, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import styles from './WorkflowDetail.module.css'

interface Step {
  step_id: string
  step_name: string
  status: string
  started_at: string | null
  completed_at: string | null
}

interface Output {
  step_name: string
  data: unknown
  created_at: string
}

interface WorkflowData {
  workflow_id: string
  workflow_type: string
  status: string
  created_at: string
  updated_at: string
  total_tasks: number
  completed_tasks: number
  steps: Step[]
  outputs: Output[]
}

const statusClass: Record<string, string> = {
  pending: styles.statusPending,
  running: styles.statusRunning,
  complete: styles.statusComplete,
  completed: styles.statusComplete,
  failed: styles.statusFailed,
}

function formatTime(iso: string | null): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString()
}

function duration(start: string | null, end: string | null): string {
  if (!start) return '—'
  const s = new Date(start).getTime()
  const e = end ? new Date(end).getTime() : Date.now()
  const ms = e - s
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60000).toFixed(1)}m`
}

function isTerminal(status: string): boolean {
  return status === 'completed' || status === 'complete' || status === 'failed'
}

function classifySteps(steps: Step[]): { preSteps: Step[]; fanOutSteps: Step[]; postSteps: Step[] } {
  const preSteps: Step[] = []
  const fanOutSteps: Step[] = []
  const postSteps: Step[] = []
  for (const step of steps) {
    if (step.step_name === 'receive' || step.step_name === 'plan') {
      preSteps.push(step)
    } else if (/^execute-\d+$/.test(step.step_name)) {
      fanOutSteps.push(step)
    } else {
      postSteps.push(step)
    }
  }
  return { preSteps, fanOutSteps, postSteps }
}

function getStepOutput(stepName: string, outputs: Output[]): unknown | null {
  const match = outputs.find(o => o.step_name === stepName)
  return match ? match.data : null
}

function StepCard({ step }: { step: Step }) {
  return (
    <div className={styles.stepCard}>
      <div className={styles.stepCardHeader}>
        <code>{step.step_name}</code>
        <span className={`${styles.stepStatus} ${statusClass[step.status] || ''}`}>
          {step.status}
        </span>
      </div>
      <div className={styles.stepCardMeta}>
        {duration(step.started_at, step.completed_at)}
      </div>
    </div>
  )
}

function ExecuteCard({ step, index, total, outputs }: { step: Step; index: number; total: number; outputs: Output[] }) {
  const output = getStepOutput(step.step_name, outputs) as { instruction?: string; result?: string } | null
  return (
    <div className={styles.stepCard}>
      <div className={styles.stepCardHeader}>
        <span>Task {index + 1} of {total}</span>
        <span className={`${styles.stepStatus} ${statusClass[step.status] || ''}`}>
          {step.status}
        </span>
      </div>
      <div className={styles.instruction}>
        {output?.instruction || 'Pending...'}
      </div>
      <div className={styles.stepCardMeta}>
        {duration(step.started_at, step.completed_at)}
      </div>
    </div>
  )
}

function SynthesizeCard({ step, completedTasks, totalTasks }: { step: Step; completedTasks: number; totalTasks: number }) {
  let info = ''
  if (step.status === 'pending') {
    info = `Waiting for ${totalTasks} tasks`
  } else if (step.status === 'running') {
    info = `Synthesizing ${completedTasks} results`
  } else if (step.status === 'complete' || step.status === 'completed') {
    info = `Synthesized ${completedTasks} results`
  }
  return (
    <div className={styles.stepCard}>
      <div className={styles.stepCardHeader}>
        <code>{step.step_name}</code>
        <span className={`${styles.stepStatus} ${statusClass[step.status] || ''}`}>
          {step.status}
        </span>
      </div>
      {info && <div className={styles.synthesizeInfo}>{info}</div>}
      <div className={styles.stepCardMeta}>
        {duration(step.started_at, step.completed_at)}
      </div>
    </div>
  )
}

export default function WorkflowDetail() {
  const { id } = useParams<{ id: string }>()
  const [workflow, setWorkflow] = useState<WorkflowData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const intervalRef = useRef<number | null>(null)

  const fetchData = useCallback(() => {
    if (!id) return
    fetch(`/api/workflows/${id}`)
      .then(r => {
        if (!r.ok) {
          if (r.status === 404) throw new Error('Workflow not found')
          throw new Error('Failed to load workflow')
        }
        return r.json()
      })
      .then((data: WorkflowData) => {
        setWorkflow(data)
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
    return <div className={styles.loading}>Loading workflow...</div>
  }

  if (error || !workflow) {
    return (
      <div>
        <Link to="/workflows" className={styles.backLink}>Back to Workflows</Link>
        <div className={styles.loading}>{error || 'Workflow not found'}</div>
      </div>
    )
  }

  const progressPct = workflow.total_tasks > 0
    ? Math.round((workflow.completed_tasks / workflow.total_tasks) * 100)
    : null

  const { preSteps, fanOutSteps, postSteps } = classifySteps(workflow.steps)
  const hasFanOut = fanOutSteps.length > 0

  return (
    <div>
      <Link to="/workflows" className={styles.backLink}>Back to Workflows</Link>

      <div className={styles.headerRow}>
        <h1 className={styles.title}>{workflow.workflow_type}</h1>
        <span className={`${styles.statusBadge} ${statusClass[workflow.status] || ''}`}>
          {workflow.status}
        </span>
      </div>

      <div className={styles.meta}>
        <div>Created: {formatTime(workflow.created_at)}</div>
        <div>Updated: {formatTime(workflow.updated_at)}</div>
        <div>Duration: {duration(workflow.created_at, isTerminal(workflow.status) ? workflow.updated_at : null)}</div>
      </div>

      {progressPct !== null && (
        <div className={styles.progressSection}>
          <div className={styles.progressLabel}>
            Progress: {workflow.completed_tasks} / {workflow.total_tasks} tasks
          </div>
          <div className={styles.progressBar}>
            <div
              className={`${styles.progressFill} ${workflow.status === 'failed' ? styles.progressFailed : ''}`}
              style={{ width: `${progressPct}%` }}
            />
          </div>
        </div>
      )}

      <h2 className={styles.sectionTitle}>Steps</h2>
      {workflow.steps.length === 0 ? (
        <div className={styles.empty}>No steps yet.</div>
      ) : (
        <div className={styles.pipeline}>
          {preSteps.map(step => (
            <div key={step.step_id} className={hasFanOut ? styles.connector : undefined}>
              <StepCard step={step} />
            </div>
          ))}
          {hasFanOut && (
            <div className={styles.connector}>
              <div className={styles.fanOutGroup}>
                {fanOutSteps.map((step, i) => (
                  <ExecuteCard
                    key={step.step_id}
                    step={step}
                    index={i}
                    total={fanOutSteps.length}
                    outputs={workflow.outputs}
                  />
                ))}
              </div>
            </div>
          )}
          {postSteps.map(step => (
            <div key={step.step_id}>
              {step.step_name === 'synthesize' ? (
                <SynthesizeCard
                  step={step}
                  completedTasks={workflow.completed_tasks}
                  totalTasks={workflow.total_tasks}
                />
              ) : (
                <StepCard step={step} />
              )}
            </div>
          ))}
        </div>
      )}

      {workflow.outputs.length > 0 && (
        <>
          <h2 className={styles.sectionTitle}>Outputs</h2>
          {workflow.outputs.map((output, i) => (
            <details key={i} className={styles.outputDetails}>
              <summary className={styles.outputSummary}>{output.step_name}</summary>
              <pre className={styles.outputPre}>
                {JSON.stringify(output.data, null, 2)}
              </pre>
            </details>
          ))}
        </>
      )}
    </div>
  )
}
