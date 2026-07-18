import { ArrowUpRight, Check, Clipboard, Hash, LockKeyhole, Trash2, X } from 'lucide-react'
import type { Artifact } from '../types'
import { formatBytes } from '../lib/format'

type ArtifactDetailsProps = {
  artifact: Artifact
  open: boolean
  copied: boolean
  onClose: () => void
  onCopy: () => void
  onDelete: () => void
}

export function ArtifactDetails({ artifact, open, copied, onClose, onCopy, onDelete }: ArtifactDetailsProps) {
  const metadata = Object.entries(artifact.metadata ?? {})
  const typeLabel = artifact.type === 'markdown' ? 'MD' : artifact.type.toUpperCase()

  return (
    <aside className={`details-panel ${open ? 'open' : ''}`} aria-label="Artifact 元数据">
      <div className="details-heading">
        <div><p className="eyebrow">Provenance</p><h3>Artifact 元数据</h3></div>
        <button type="button" aria-label="关闭元数据" onClick={onClose}><X size={17} /></button>
      </div>
      <section className="details-section artifact-identity">
        <span className={`artifact-type-mark ${artifact.type}`}>{typeLabel}</span>
        <div><strong>{artifact.title}</strong><span>{artifact.originalFilename}</span></div>
      </section>
      <section className="details-section">
        <Detail label="Format" value={`${artifact.type.toUpperCase()} · ${artifact.mediaType}`} />
        <Detail label="Size" value={formatBytes(artifact.sizeBytes)} />
        <Detail label="Created" value={new Date(artifact.createdAt).toLocaleString('zh-CN')} />
      </section>
      <section className="details-section">
        <div className="detail-label">Tags</div>
        <div className="tag-cloud">{artifact.tags.length ? artifact.tags.map((tag) => <span className="tag" key={tag}>{tag}</span>) : <span className="muted">没有标签</span>}</div>
      </section>
      <section className="details-section checksum">
        <div className="detail-label"><Hash size={12} /> SHA-256 fingerprint</div>
        <code>{artifact.sha256}</code>
      </section>
      {!!metadata.length && (
        <section className="details-section custom-metadata">
          <div className="detail-label">Custom metadata</div>
          {metadata.map(([key, value]) => <Detail key={key} label={key} value={typeof value === 'string' ? value : JSON.stringify(value)} mono />)}
        </section>
      )}
      <div className="details-spacer" />
      <section className="details-section public-link">
        <div className="detail-label"><LockKeyhole size={12} /> Stable public URL</div>
        <div className="public-link-actions">
          <button type="button" onClick={onCopy} aria-label="复制稳定链接"><span>{artifact.publicUrl.replace(/^https?:\/\//, '')}</span>{copied ? <Check size={15} /> : <Clipboard size={15} />}</button>
          <a href={artifact.publicUrl} target="_blank" rel="noreferrer" aria-label="在新标签页打开 Artifact"><ArrowUpRight size={15} /></a>
        </div>
        <p><LockKeyhole size={11} /> 内容与地址保持稳定，直到你主动删除它。</p>
      </section>
      <section className="details-danger-zone">
        <button type="button" onClick={onDelete}><Trash2 size={14} /> 删除这个 Artifact</button>
      </section>
    </aside>
  )
}

function Detail({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="detail-row"><span>{label}</span><strong className={mono ? 'mono' : ''}>{value}</strong></div>
}
