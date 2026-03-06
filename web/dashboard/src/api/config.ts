import { get } from './http'

export const fetchConfig = () => get<Record<string, any>>('/config')
