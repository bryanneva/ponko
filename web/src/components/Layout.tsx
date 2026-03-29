import { NavLink, Outlet } from 'react-router-dom'
import type { User } from '../auth'
import styles from './Layout.module.css'

export default function Layout({ user }: { user: User }) {

  function handleLogout() {
    fetch('/api/auth/logout', { method: 'POST' })
      .then(() => window.location.assign('/login'))
      .catch(() => window.location.assign('/login'))
  }

  return (
    <div className={styles.layout}>
      <aside className={styles.sidebar}>
        <div className={styles.logo}>Ponko</div>
        <nav className={styles.nav}>
          <NavLink
            to="/channels"
            className={({ isActive }) =>
              `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
            }
          >
            Channels
          </NavLink>
          <NavLink
            to="/jobs"
            className={({ isActive }) =>
              `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
            }
          >
            Jobs
          </NavLink>
          <NavLink
            to="/workflows"
            className={({ isActive }) =>
              `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
            }
          >
            Workflows
          </NavLink>
          <NavLink
            to="/conversations"
            className={({ isActive }) =>
              `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
            }
          >
            Conversations
          </NavLink>
          <NavLink
            to="/recipes"
            className={({ isActive }) =>
              `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
            }
          >
            Recipes
          </NavLink>
          <NavLink
            to="/tools"
            className={({ isActive }) =>
              `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
            }
          >
            Tools
          </NavLink>
        </nav>
        <div className={styles.userSection}>
          <div className={styles.userName}>{user.displayName}</div>
          <button className={styles.logoutButton} onClick={handleLogout}>
            Sign out
          </button>
        </div>
      </aside>
      <main className={styles.content}>
        <Outlet />
      </main>
    </div>
  )
}
