import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, Braces, Check, ChevronDown, FileCode2, FileText, LockKeyhole, Table2, Trash2, UploadCloud, X } from 'lucide-react'
import { api } from '../api'
import type { Artifact, Collection } from '../types'
import { formatBytes } from '../lib/format'

const colors = ['#7C6DF2', '#35B786', '#E49B52', '#E45C66', '#45A5DE']
const maxArtifactSize = 10 * 1024 * 1024
const supportedArtifact = /\.(html?|md|markdown|json|csv)$/i

function fileKind(filename: string) {
  if (/\.html?$/i.test(filename)) return 'html'
  if (/\.json$/i.test(filename)) return 'json'
  if (/\.csv$/i.test(filename)) return 'csv'
  return 'markdown'
}

function UploadFileIcon({ filename }: { filename: string }) {
  const kind = fileKind(filename)
  if (kind === 'html') return <FileCode2 size={20} />
  if (kind === 'json') return <Braces size={20} />
  if (kind === 'csv') return <Table2 size={20} />
  return <FileText size={20} />
}

export function CollectionModal({ onClose, onCreated }: { onClose: () => void; onCreated: (collection: Collection) => void }) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [color, setColor] = useState(colors[0])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    setSaving(true)
    try { onCreated(await api.createCollection({ name, description, color })) }
    catch (reason) { setError(reason instanceof Error ? reason.message : '创建失败'); setSaving(false) }
  }

  return (
    <ModalFrame title="新建 Collection" subtitle="为一组长期可引用的产物建立清晰边界。" onClose={onClose}>
      <form onSubmit={submit}>
        <Field label="名称"><input autoFocus required maxLength={120} value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：产品发布" /></Field>
        <Field label="描述"><textarea rows={3} value={description} onChange={(event) => setDescription(event.target.value)} placeholder="这一组内容属于什么工作流？" /></Field>
        <Field label="标识颜色"><div className="color-picker">{colors.map((item) => <button type="button" aria-label={`选择颜色 ${item}`} className={color === item ? 'selected' : ''} style={{ background: item }} onClick={() => setColor(item)} key={item} />)}</div></Field>
        {error && <p className="form-error">{error}</p>}
        <ModalActions saving={saving} onClose={onClose} action="创建 Collection" />
      </form>
    </ModalFrame>
  )
}

export function ArtifactModal({ collection, onClose, onCreated }: { collection: Collection; onClose: () => void; onCreated: (artifact: Artifact) => void }) {
  const [file, setFile] = useState<File | null>(null)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [tags, setTags] = useState('')
  const [metadata, setMetadata] = useState('{}')
  const [saving, setSaving] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  const pickFile = (next: File | null) => {
    if (!next) return
    if (!supportedArtifact.test(next.name)) {
      setError('仅支持 HTML、Markdown、JSON 与 CSV 文件')
      return
    }
    if (next.size > maxArtifactSize) {
      setError('Artifact 不能超过 10 MB')
      return
    }
    setError('')
    setFile(next)
    if (!title) setTitle(next.name.replace(/\.(html?|md|markdown|json|csv)$/i, ''))
  }

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!file) { setError('请选择一个文件'); return }
    try { JSON.parse(metadata) } catch { setError('自定义元数据必须是有效 JSON'); return }
    const form = new FormData()
    form.set('file', file)
    form.set('title', title)
    form.set('description', description)
    form.set('tags', tags)
    form.set('metadata', metadata)
    setSaving(true)
    try { onCreated(await api.uploadArtifact(collection.id, form)) }
    catch (reason) { setError(reason instanceof Error ? reason.message : '上传失败'); setSaving(false) }
  }

  return (
    <ModalFrame title="发布 Artifact" subtitle={`发布到 ${collection.name}。内容与元数据将生成永久指纹。`} onClose={onClose} wide>
      <form onSubmit={submit}>
        <input ref={inputRef} className="visually-hidden" type="file" accept=".html,.htm,.md,.markdown,.json,.csv,text/html,text/markdown,application/json,text/csv" onChange={(event) => pickFile(event.target.files?.[0] ?? null)} />
        <button type="button" className={`drop-zone ${file ? 'has-file' : ''} ${dragging ? 'is-dragging' : ''}`} onClick={() => inputRef.current?.click()} onDragEnter={(event) => { event.preventDefault(); setDragging(true) }} onDragOver={(event) => event.preventDefault()} onDragLeave={() => setDragging(false)} onDrop={(event) => { event.preventDefault(); setDragging(false); pickFile(event.dataTransfer.files[0] ?? null) }}>
          {file ? <><span className={`upload-file-icon ${fileKind(file.name)}`}><UploadFileIcon filename={file.name} /></span><div><strong>{file.name}</strong><span>{formatBytes(file.size)} · 点击更换</span></div><Check size={18} className="upload-check" /></> : <><UploadCloud size={24} /><strong>拖入 HTML、Markdown、JSON 或 CSV</strong><span>或点击浏览 · 最大 10 MB</span></>}
        </button>
        <div className="form-grid">
          <Field label="标题"><input required maxLength={200} value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Artifact 标题" /></Field>
          <Field label="标签"><input value={tags} onChange={(event) => setTags(event.target.value)} placeholder="release, docs, v1" /></Field>
        </div>
        <Field label="描述"><input value={description} onChange={(event) => setDescription(event.target.value)} placeholder="一句话说明这个 artifact" /></Field>
        <details className="advanced-fields">
          <summary><span>高级元数据</span><small>JSON</small><ChevronDown size={14} /></summary>
          <div><Field label="自定义元数据（JSON）"><textarea className="code-input" rows={4} value={metadata} onChange={(event) => setMetadata(event.target.value)} spellCheck={false} /></Field></div>
        </details>
        <div className="immutable-callout"><LockKeyhole size={16} /><div><strong>Immutable by design</strong><span>上传后不可覆盖或编辑；任何变化都会成为一个新的 artifact。</span></div></div>
        {error && <p className="form-error">{error}</p>}
        <ModalActions saving={saving} onClose={onClose} action="发布 Artifact" />
      </form>
    </ModalFrame>
  )
}

export function DeleteArtifactModal({ artifact, onClose, onConfirm }: { artifact: Artifact; onClose: () => void; onConfirm: () => Promise<boolean> }) {
  const [deleting, setDeleting] = useState(false)
  const confirm = async () => {
    setDeleting(true)
    if (!await onConfirm()) setDeleting(false)
  }

  return (
    <ModalFrame title="永久删除 Artifact" subtitle="这是唯一不可恢复的生命周期操作。" onClose={onClose}>
      <div className="delete-confirmation">
        <div className="delete-confirmation-icon"><AlertTriangle size={20} /></div>
        <div><strong>{artifact.title}</strong><span>{artifact.originalFilename}</span></div>
      </div>
      <p className="delete-warning">内容、元数据与稳定链接都会立即失效。其他地方保存的引用也将无法继续访问。</p>
      <div className="modal-actions"><button type="button" className="ghost-button" onClick={onClose}>保留 Artifact</button><button type="button" className="destructive-button" disabled={deleting} onClick={confirm}><Trash2 size={14} />{deleting ? '正在删除…' : '永久删除'}</button></div>
    </ModalFrame>
  )
}

function ModalFrame({ title, subtitle, onClose, wide, children }: { title: string; subtitle: string; onClose: () => void; wide?: boolean; children: React.ReactNode }) {
  const frameRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null
    const focusable = () => Array.from(frameRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), textarea:not([disabled]), summary, a[href]') ?? [])
    const animationFrame = window.requestAnimationFrame(() => {
      const preferred = frameRef.current?.querySelector<HTMLElement>('[autofocus]')
      const target = preferred ?? focusable()[0]
      target?.focus()
    })
    const trapFocus = (event: KeyboardEvent) => {
      if (event.key !== 'Tab') return
      const items = focusable()
      if (!items.length) return
      const first = items[0]
      const last = items[items.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', trapFocus)
    return () => {
      window.cancelAnimationFrame(animationFrame)
      document.removeEventListener('keydown', trapFocus)
      previouslyFocused?.focus()
    }
  }, [])

  return <div className="modal-backdrop" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><div ref={frameRef} className={`modal ${wide ? 'wide' : ''}`} role="dialog" aria-modal="true" aria-labelledby="modal-title"><header><div><h2 id="modal-title">{title}</h2><p>{subtitle}</p></div><button type="button" aria-label="关闭" onClick={onClose}><X size={18} /></button></header>{children}</div></div>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="field"><span>{label}</span>{children}</label>
}

function ModalActions({ saving, onClose, action }: { saving: boolean; onClose: () => void; action: string }) {
  return <div className="modal-actions"><button type="button" className="ghost-button" onClick={onClose}>取消</button><button className="primary-button" disabled={saving}>{saving ? '处理中…' : action}</button></div>
}
