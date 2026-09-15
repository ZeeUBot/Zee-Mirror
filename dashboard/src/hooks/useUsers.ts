import { useState, useEffect, useCallback } from 'react'
import api from '../api'
import { User } from '../types'

export const useUsers = (apiToken: string) => {
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState<boolean>(false)
  const [error, setError] = useState<string | null>(null)

  const fetchUsers = useCallback(async () => {
    if (!apiToken) return
    setLoading(true)
    try {
      const response = await api.get('/api/users')
      setUsers(response.data || [])
      setError(null)
    } catch {
      setError('Failed to fetch users')
    } finally {
      setLoading(false)
    }
  }, [apiToken])

  const updateUser = async (userData: Record<string, unknown>): Promise<{ success: boolean; error?: string; apiKey?: string }> => {
    try {
      const response = await api.post('/api/users/update', userData)
      if (response.status >= 200 && response.status < 300) {
        await fetchUsers()
        return { success: true, apiKey: (response.data as { apiKey?: string }).apiKey }
      }
      return { success: false, error: 'Update failed' }
    } catch {
      return { success: false, error: 'Network error' }
    }
  }

  const deleteUser = async (id: number): Promise<{ success: boolean; error?: string }> => {
    try {
      const response = await api.post('/api/users/delete', { id })
      if (response.status === 200) {
        await fetchUsers()
        return { success: true }
      }
      return { success: false, error: 'Delete failed' }
    } catch {
      return { success: false, error: 'Network error' }
    }
  }

  const addUser = async (userData: Record<string, unknown>): Promise<{ success: boolean; error?: string; apiKey?: string }> => {
    try {
      const response = await api.post('/api/users/add', userData)
      if (response.status >= 200 && response.status < 300) {
        await fetchUsers()
        return { success: true, apiKey: (response.data as { apiKey?: string }).apiKey }
      }
      return { success: false, error: (response.data as { error?: string }).error || 'Add failed' }
    } catch {
      return { success: false, error: 'Network error' }
    }
  }

  useEffect(() => {
    fetchUsers()
  }, [fetchUsers])

  return { users, loading, error, fetchUsers, updateUser, deleteUser, addUser }
}
