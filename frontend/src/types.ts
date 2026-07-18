export type Collection = {
  id: string
  slug: string
  name: string
  description: string
  color: string
  artifactCount: number
  createdAt: string
}

export type Artifact = {
  id: string
  collectionId: string
  collectionName?: string
  slug: string
  title: string
  description: string
  type: 'html' | 'markdown' | 'json' | 'csv'
  mediaType: string
  originalFilename: string
  sizeBytes: number
  sha256: string
  tags: string[]
  metadata: Record<string, unknown>
  createdAt: string
  contentUrl: string
  publicUrl: string
}
