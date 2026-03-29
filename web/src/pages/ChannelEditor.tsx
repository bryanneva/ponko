import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import styles from './ChannelEditor.module.css'

interface ChannelConfig {
  channel_id: string
  channel_name?: string
  system_prompt: string
  respond_mode: string
  tool_allowlist: string[] | null
  approval_required: boolean
}

export default function ChannelEditor() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [loading, setLoading] = useState(true)
  const [loadFailed, setLoadFailed] = useState(false)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<{ text: string; error: boolean } | null>(null)

  const [channelName, setChannelName] = useState('')
  const [systemPrompt, setSystemPrompt] = useState('')
  const [respondMode, setRespondMode] = useState('mention_only')
  const [approvalRequired, setApprovalRequired] = useState(false)
  const [toolMode, setToolMode] = useState<'all' | 'select' | 'none'>('all')
  const [selectedTools, setSelectedTools] = useState<Set<string>>(new Set())
  const [allTools, setAllTools] = useState<string[]>([])

  useEffect(() => {
    Promise.all([
      fetch(`/api/channels/${id}/config`).then(r => r.json()),
      fetch('/api/tools').then(r => r.json()),
    ])
      .then(([config, toolsData]: [ChannelConfig, { tools: string[] }]) => {
        setChannelName(config.channel_name || '')
        setSystemPrompt(config.system_prompt || '')
        setRespondMode(config.respond_mode || 'mention_only')
        setApprovalRequired(config.approval_required || false)
        setAllTools(toolsData.tools || [])

        if (config.tool_allowlist == null) {
          setToolMode('all')
        } else if (config.tool_allowlist.length === 0) {
          setToolMode('none')
        } else {
          setToolMode('select')
          setSelectedTools(new Set(config.tool_allowlist))
        }
        setLoading(false)
      })
      .catch(() => {
        setMessage({ text: 'Failed to load config', error: true })
        setLoadFailed(true)
        setLoading(false)
      })
  }, [id])

  function toggleTool(tool: string) {
    setSelectedTools(prev => {
      const next = new Set(prev)
      if (next.has(tool)) {
        next.delete(tool)
      } else {
        next.add(tool)
      }
      return next
    })
  }

  async function handleSave() {
    setSaving(true)
    setMessage(null)

    let toolAllowlist: string[] | null = null
    if (toolMode === 'none') toolAllowlist = []
    if (toolMode === 'select') toolAllowlist = Array.from(selectedTools)

    try {
      const res = await fetch(`/api/channels/${id}/config`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          system_prompt: systemPrompt,
          respond_mode: respondMode,
          tool_allowlist: toolAllowlist,
          approval_required: approvalRequired,
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'Unknown error' }))
        throw new Error(data.error || `HTTP ${res.status}`)
      }
      setMessage({ text: 'Saved successfully', error: false })
    } catch (e) {
      setMessage({ text: `Failed to save: ${(e as Error).message}`, error: true })
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return <div className={styles.loading}>Loading config...</div>
  }

  return (
    <div>
      <div className={styles.header}>
        <button className={styles.backButton} onClick={() => navigate('/channels')}>
          &larr; Back to channels
        </button>
        <h1 className={styles.title}>Channel: {channelName ? `#${channelName}` : <code>{id}</code>}</h1>
      </div>

      <div className={styles.form}>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="systemPrompt">System Prompt</label>
          <textarea
            id="systemPrompt"
            className={styles.textarea}
            value={systemPrompt}
            onChange={e => setSystemPrompt(e.target.value)}
            placeholder="Leave empty to use the global default..."
          />
        </div>

        <div className={styles.field}>
          <label className={styles.label}>Respond Mode</label>
          <select
            className={styles.select}
            value={respondMode}
            onChange={e => setRespondMode(e.target.value)}
          >
            <option value="mention_only">Mention only</option>
            <option value="all_messages">All messages</option>
          </select>
        </div>

        <div className={styles.field}>
          <label className={styles.checkboxLabel}>
            <input
              type="checkbox"
              checked={approvalRequired}
              onChange={e => setApprovalRequired(e.target.checked)}
            />
            Require approval for multi-step plans
          </label>
          <span className={styles.hint}>
            When enabled, the bot will show Approve/Reject buttons before executing fan-out plans.
          </span>
        </div>

        <div className={styles.field}>
          <label className={styles.label}>Tool Access</label>
          <div className={styles.toolModeRow}>
            <label className={styles.radioLabel}>
              <input
                type="radio"
                name="toolMode"
                value="all"
                checked={toolMode === 'all'}
                onChange={() => setToolMode('all')}
              />
              All tools
            </label>
            <label className={styles.radioLabel}>
              <input
                type="radio"
                name="toolMode"
                value="select"
                checked={toolMode === 'select'}
                onChange={() => setToolMode('select')}
              />
              Selected tools only
            </label>
            <label className={styles.radioLabel}>
              <input
                type="radio"
                name="toolMode"
                value="none"
                checked={toolMode === 'none'}
                onChange={() => setToolMode('none')}
              />
              No tools
            </label>
          </div>

          {toolMode === 'select' && (
            <div className={styles.toolsGrid}>
              {allTools.map(tool => (
                <label key={tool} className={styles.toolItem}>
                  <input
                    type="checkbox"
                    checked={selectedTools.has(tool)}
                    onChange={() => toggleTool(tool)}
                  />
                  {tool}
                </label>
              ))}
            </div>
          )}
        </div>

        <div className={styles.actions}>
          <button
            className={styles.saveButton}
            onClick={handleSave}
            disabled={saving || loadFailed}
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
          {message && (
            <span className={message.error ? styles.error : styles.success}>
              {message.text}
            </span>
          )}
        </div>
      </div>
    </div>
  )
}
