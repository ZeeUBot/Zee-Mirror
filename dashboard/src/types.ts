export interface Task {
    id: string
    file_name: string
    url: string
    status: 'downloading' | 'uploading' | 'queued' | 'completed' | 'failed'
    size: number
    downloaded: number
    speed: number
    eta: number
    progress: number
    error?: string
}

export interface SystemMetrics {
    cpu: number
    ram: number
    disk: number
    uptime: number
    os: string
    arch: string
}

export interface Stats {
    total_tasks: number
    total_bandwidth: number
    users_count: number
}

export interface User {
    id: number
    username: string
    role: 'admin' | 'user' | 'owner' | 'authorized'
    status: 'active' | 'banned'
    maxDailyTasks: number
    maxDailyBandwidth: number
    expiresAt?: { Valid: boolean; Time: string }
    usedTasks?: number
    usedBandwidth?: number
    apiKey?: string
}

export interface FileItem {
    name: string
    displayName?: string
    path?: string
    size: number
    isDir: boolean
    time?: string
    mod_time?: string
}

export type Theme = 'light' | 'dark'

export interface ApiResponse<T> {
    data: T
    status: 'success' | 'error'
    message?: string
}
