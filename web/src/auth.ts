import { useState, useEffect } from 'react'

export interface User {
  userId: string
  displayName: string
}

type AuthState =
  | { status: 'loading' }
  | { status: 'authenticated'; user: User }
  | { status: 'unauthenticated' }

export function useAuth(): AuthState {
  const [state, setState] = useState<AuthState>({ status: 'loading' })

  useEffect(() => {
    fetch('/api/auth/me')
      .then((res) => {
        if (!res.ok) {
          setState({ status: 'unauthenticated' })
          return
        }
        return res.json().then((user: User) => {
          setState({ status: 'authenticated', user })
        })
      })
      .catch(() => {
        setState({ status: 'unauthenticated' })
      })
  }, [])

  return state
}
