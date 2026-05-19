import fs from 'fs'
import path from 'path'

const ADMIN_FILE = path.join(__dirname, '..', '.auth', 'admin.json')

export interface AdminCreds {
  token: string
  email: string
  password: string
  first_name: string
  last_name: string
}

export function getAdminCreds(): AdminCreds {
  return JSON.parse(fs.readFileSync(ADMIN_FILE, 'utf-8'))
}

export function getAdminToken(): string {
  return getAdminCreds().token
}

export function adminHeaders(): { Authorization: string } {
  return { Authorization: `Bearer ${getAdminToken()}` }
}
