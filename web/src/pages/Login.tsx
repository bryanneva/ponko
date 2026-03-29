import styles from './Login.module.css'

export default function Login() {
  return (
    <div className={styles.container}>
      <div className={styles.card}>
        <h1 className={styles.title}>Ponko</h1>
        <p className={styles.subtitle}>Sign in to access the dashboard</p>
        <a href="/api/auth/slack" className={styles.slackButton}>
          Sign in with Slack
        </a>
      </div>
    </div>
  )
}
