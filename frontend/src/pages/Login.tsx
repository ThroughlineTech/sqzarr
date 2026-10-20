import { useState, useEffect, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'

export function Login() {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [checking, setChecking] = useState(true)
  const navigate = useNavigate()

  // If no password is configured, skip login entirely.
  useEffect(() => {
    api.getConfig()
      .then(cfg => {
        if (!cfg.has_password) {
          localStorage.removeItem('sqzarr_token')
          navigate('/', { replace: true })
        } else {
          setChecking(false)
        }
      })
      .catch(() => {
        // Config returned 401 → auth is required, show the form.
        setChecking(false)
      })
  }, [navigate])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const { token } = await api.login(password)
      localStorage.setItem('sqzarr_token', token)
      navigate('/')
    } catch {
      setError('Incorrect password')
    } finally {
      setLoading(false)
    }
  }

  if (checking) return null

  return (
    <div className="min-h-screen bg-stone-50 flex items-center justify-center">
      <div className="w-full max-w-sm space-y-6 p-8">
        <div className="text-center">
          <h1 className="text-2xl font-semibold text-stone-900 tracking-tight">SQZARR</h1>
          <p className="text-sm text-stone-500 mt-1">Media Transcoder</p>
        </div>

        <form onSubmit={submit} className="space-y-4">
          <div>
            <input
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              placeholder="Password"
              autoFocus
              required
              className={`block w-full rounded-md border px-3 py-2 text-sm text-stone-900 placeholder-stone-400 focus:outline-hidden focus:ring-1 ${
                error
                  ? 'border-red-400 focus:border-red-400 focus:ring-red-400'
                  : 'border-stone-300 focus:border-amber-500 focus:ring-amber-500'
              }`}
            />
            {error && <p className="mt-1.5 text-xs text-red-600">{error}</p>}
          </div>

          <button
            type="submit"
            disabled={loading}
            className="w-full rounded-md bg-stone-800 px-4 py-2.5 text-sm font-medium text-white hover:bg-stone-700 disabled:opacity-50 transition-colors flex items-center justify-center gap-2"
          >
            {loading && (
              <svg className="animate-spin h-4 w-4 text-white" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.4 0 0 5.4 0 12h4z" />
              </svg>
            )}
            Sign In
          </button>
        </form>
      </div>
    </div>
  )
}
