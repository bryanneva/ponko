import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import styles from './Recipes.module.css'

interface RecipeField {
  name: string
  label: string
  type: string
  required: boolean
  placeholder: string
}

interface Recipe {
  id: string
  name: string
  description: string
  fields: RecipeField[]
}

export default function Recipes() {
  const [recipes, setRecipes] = useState<Recipe[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [fieldValues, setFieldValues] = useState<Record<string, string>>({})
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const navigate = useNavigate()

  useEffect(() => {
    fetch('/api/recipes')
      .then(r => r.json())
      .then((data: { recipes: Recipe[] }) => {
        setRecipes(data.recipes)
        setLoading(false)
      })
      .catch(() => {
        setError(true)
        setLoading(false)
      })
  }, [])

  function handleCardClick(recipe: Recipe) {
    if (expandedId === recipe.id) {
      setExpandedId(null)
      setFieldValues({})
      setSubmitError('')
      return
    }
    setExpandedId(recipe.id)
    setFieldValues({})
    setSubmitError('')
  }

  function handleFieldChange(fieldName: string, value: string) {
    setFieldValues(prev => ({ ...prev, [fieldName]: value }))
  }

  function handleSubmit(recipe: Recipe) {
    setSubmitting(true)
    setSubmitError('')

    const body: Record<string, string> = {}
    for (const field of recipe.fields) {
      body[field.name] = fieldValues[field.name] || ''
    }

    fetch(`/api/recipes/${recipe.id}/run`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
      .then(async r => {
        if (!r.ok) {
          const data = await r.json().catch(() => null)
          throw new Error(data?.error || `Request failed: ${r.status}`)
        }
        return r.json()
      })
      .then((data: { workflow_id: string }) => {
        navigate(`/workflows/${data.workflow_id}`)
      })
      .catch((err: Error) => {
        setSubmitError(err.message || 'Failed to run recipe.')
        setSubmitting(false)
      })
  }

  if (loading) {
    return <div className={styles.loading}>Loading recipes...</div>
  }

  if (error) {
    return (
      <div>
        <div className={styles.header}>
          <h1 className={styles.title}>Recipes</h1>
        </div>
        <div className={styles.empty}>Failed to load recipes.</div>
      </div>
    )
  }

  if (recipes.length === 0) {
    return (
      <div>
        <div className={styles.header}>
          <h1 className={styles.title}>Recipes</h1>
        </div>
        <div className={styles.empty}>No recipes available.</div>
      </div>
    )
  }

  return (
    <div>
      <div className={styles.header}>
        <h1 className={styles.title}>Recipes</h1>
      </div>
      <div className={styles.grid}>
        {recipes.map(recipe => {
          const isExpanded = expandedId === recipe.id
          return (
            <div
              key={recipe.id}
              className={isExpanded ? styles.cardExpanded : styles.card}
              onClick={!isExpanded ? () => handleCardClick(recipe) : undefined}
            >
              <h2 className={styles.cardName}>{recipe.name}</h2>
              <p className={styles.cardDescription}>{recipe.description}</p>
              {isExpanded && (
                <div className={styles.form}>
                  {recipe.fields.map(field => (
                    <div key={field.name} className={styles.fieldGroup}>
                      <label className={styles.label} htmlFor={`${recipe.id}-${field.name}`}>
                        {field.label}
                        {field.required && <span className={styles.required}>*</span>}
                      </label>
                      {field.type === 'textarea' ? (
                        <textarea
                          id={`${recipe.id}-${field.name}`}
                          className={styles.textarea}
                          placeholder={field.placeholder}
                          value={fieldValues[field.name] || ''}
                          onChange={e => handleFieldChange(field.name, e.target.value)}
                        />
                      ) : (
                        <input
                          id={`${recipe.id}-${field.name}`}
                          className={styles.input}
                          type="text"
                          placeholder={field.placeholder}
                          value={fieldValues[field.name] || ''}
                          onChange={e => handleFieldChange(field.name, e.target.value)}
                        />
                      )}
                    </div>
                  ))}
                  {submitError && <div className={styles.error}>{submitError}</div>}
                  <div className={styles.actions}>
                    <button
                      className={styles.submitBtn}
                      disabled={submitting}
                      onClick={() => handleSubmit(recipe)}
                    >
                      {submitting ? 'Running...' : 'Run Recipe'}
                    </button>
                    <button
                      className={styles.cancelBtn}
                      onClick={() => handleCardClick(recipe)}
                    >
                      Cancel
                    </button>
                  </div>
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
