import type { Artifact, Collection } from './types'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }))
    throw new Error(body.error || '请求失败')
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const api = {
  collections: () => request<Collection[]>('/api/collections'),
  createCollection: (input: Pick<Collection, 'name' | 'description' | 'color'>) =>
    request<Collection>('/api/collections', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    }),
  artifacts: (collectionId: string, query = '', signal?: AbortSignal) =>
    request<Artifact[]>(`/api/collections/${collectionId}/artifacts?q=${encodeURIComponent(query)}`, { signal }),
  artifact: (artifactId: string) => request<Artifact>(`/api/artifacts/${artifactId}`),
  artifactVersions: (artifactId: string) => request<Artifact[]>(`/api/artifacts/${artifactId}/versions`),
  artifactContent: async (artifactId: string, signal?: AbortSignal) => {
    const response = await fetch(`/api/artifacts/${artifactId}/content`, { signal })
    if (!response.ok) throw new Error('无法读取 artifact 内容')
    return response.text()
  },
  uploadArtifact: (collectionId: string, form: FormData) =>
    request<Artifact>(`/api/collections/${collectionId}/artifacts`, { method: 'POST', body: form }),
  deleteArtifact: (artifactId: string) => request<void>(`/api/artifacts/${artifactId}`, { method: 'DELETE' }),
}
