import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Box,
  Check,
  ChevronRight,
  Clipboard,
  Code2,
  FileCode2,
  FileText,
  FolderPlus,
  Hash,
  Info,
  LockKeyhole,
  Maximize2,
  MoreHorizontal,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  Search,
  ShieldCheck,
  Trash2,
  UploadCloud,
  X,
} from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api } from './api'
import type { Artifact, Collection } from './types'

const colors = ['#5E6AD2', '#3E9B6F', '#D2914B', '#C24444', '#4C9BD9']

type Modal = 'collection' | 'artifact' | null

function App() {
  const [collections, setCollections] = useState<Collection[]>([])
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [collectionId, setCollectionId] = useState<string | null>(null)
  const [artifactId, setArtifactId] = useState<string | null>(null)
  const [markdown, setMarkdown] = useState('')
  const [query, setQuery] = useState('')
  const [modal, setModal] = useState<Modal>(null)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [collectionsCollapsed, setCollectionsCollapsed] = useState(false)
  const [artifactsCollapsed, setArtifactsCollapsed] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const selectedCollection = collections.find((item) => item.id === collectionId) ?? null
  const selectedArtifact = artifacts.find((item) => item.id === artifactId) ?? null

  const refreshCollections = useCallback(async () => {
    const next = await api.collections()
    setCollections(next)
    setCollectionId((current) => current && next.some((item) => item.id === current) ? current : next[0]?.id ?? null)
  }, [])

  useEffect(() => {
    refreshCollections().catch((reason) => setError(reason.message)).finally(() => setLoading(false))
  }, [refreshCollections])

  useEffect(() => {
    if (!collectionId) {
      setArtifacts([])
      setArtifactId(null)
      return
    }
    const timer = window.setTimeout(() => {
      api.artifacts(collectionId, query)
        .then((next) => {
          setArtifacts(next)
          setArtifactId((current) => current && next.some((item) => item.id === current) ? current : next[0]?.id ?? null)
        })
        .catch((reason) => setError(reason.message))
    }, query ? 180 : 0)
    return () => window.clearTimeout(timer)
  }, [collectionId, query])

  useEffect(() => {
    if (selectedArtifact?.type !== 'markdown') {
      setMarkdown('')
      return
    }
    api.artifactContent(selectedArtifact.id).then(setMarkdown).catch((reason) => setError(reason.message))
  }, [selectedArtifact?.id, selectedArtifact?.type])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setModal(null)
        setFullscreen(false)
        setDetailsOpen(false)
      }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        document.querySelector<HTMLInputElement>('#artifact-search')?.focus()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  const selectCollection = (id: string) => {
    setCollectionId(id)
    setQuery('')
    setDetailsOpen(false)
  }

  const afterCollectionCreated = async (collection: Collection) => {
    await refreshCollections()
    selectCollection(collection.id)
    setModal(null)
  }

  const afterArtifactCreated = async (artifact: Artifact) => {
    const next = await api.artifacts(artifact.collectionId)
    setArtifacts(next)
    setArtifactId(artifact.id)
    await refreshCollections()
    setModal(null)
  }

  const removeArtifact = async () => {
    if (!selectedArtifact || !window.confirm(`删除“${selectedArtifact.title}”？该操作无法撤销。`)) return
    try {
      await api.deleteArtifact(selectedArtifact.id)
      const next = await api.artifacts(selectedArtifact.collectionId, query)
      setArtifacts(next)
      setArtifactId(next[0]?.id ?? null)
      await refreshCollections()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '删除失败')
    }
  }

  const copyLink = async () => {
    if (!selectedArtifact) return
    await navigator.clipboard.writeText(selectedArtifact.publicUrl)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1600)
  }

  if (loading) return <LoadingScreen />

  return (
    <div className={`app-shell ${fullscreen ? 'is-fullscreen' : ''} ${collectionsCollapsed ? 'collections-collapsed' : ''} ${artifactsCollapsed ? 'artifacts-collapsed' : ''}`}>
      {!fullscreen && (
        <aside className="collections-panel">
          {collectionsCollapsed ? (
            <SidebarRail label="Collections" onExpand={() => setCollectionsCollapsed(false)} />
          ) : (
            <>
              <div className="brand">
                <div className="brand-mark"><Box size={14} strokeWidth={2.4} /></div>
                <div className="brand-copy"><strong>Artifact Hub</strong><span>immutable archive</span></div>
                <CollapseButton label="收起 Collection 侧边栏" onClick={() => setCollectionsCollapsed(true)} />
              </div>
              <div className="global-search">
                <Search size={14} />
                <input id="artifact-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索 artifact" />
                <kbd>⌘K</kbd>
              </div>
              <div className="panel-label"><span>Collections</span><span>{collections.length}</span></div>
              <nav className="collections-list">
                {collections.map((collection) => (
                  <button className={collection.id === collectionId ? 'collection active' : 'collection'} onClick={() => selectCollection(collection.id)} key={collection.id}>
                    <span className="collection-dot" style={{ background: collection.color }} />
                    <span className="collection-name">{collection.name}</span>
                    <span className="collection-count">{collection.artifactCount}</span>
                    <ChevronRight size={13} className="collection-chevron" />
                  </button>
                ))}
                <button className="new-collection" onClick={() => setModal('collection')}><FolderPlus size={14} /> 新建 Collection</button>
              </nav>
              <div className="storage-note"><ShieldCheck size={14} /><span>内容由你的 Postgres 持久化</span></div>
            </>
          )}
        </aside>
      )}

      {!fullscreen && (
        <section className="artifact-panel">
          {artifactsCollapsed ? (
            <SidebarRail label="Artifacts" onExpand={() => setArtifactsCollapsed(false)} />
          ) : (
            <>
              <header className="artifact-list-header">
                <div>
                  <p className="eyebrow">Collection</p>
                  <h1>{selectedCollection?.name ?? '尚无 Collection'}</h1>
                  <p>{selectedCollection?.description || '创建一个 collection，开始保存你的工作。'}</p>
                </div>
                <div className="artifact-header-actions">
                  {selectedCollection && <button className="icon-primary" aria-label="上传 artifact" onClick={() => setModal('artifact')}><Plus size={16} /></button>}
                  <CollapseButton label="收起 Artifact 侧边栏" onClick={() => setArtifactsCollapsed(true)} />
                </div>
              </header>
              <div className="artifact-list">
                {artifacts.map((artifact) => (
                  <button className={artifact.id === artifactId ? 'artifact-row active' : 'artifact-row'} key={artifact.id} onClick={() => { setArtifactId(artifact.id); setDetailsOpen(false) }}>
                    <div className="artifact-row-top">
                      <span className={`file-icon ${artifact.type}`}>{artifact.type === 'html' ? <FileCode2 size={15} /> : <FileText size={15} />}</span>
                      <strong>{artifact.title}</strong>
                      <span className="type-pill">{artifact.type === 'html' ? 'HTML' : 'MD'}</span>
                    </div>
                    <p>{artifact.description || artifact.originalFilename}</p>
                    <div className="artifact-row-bottom">
                      <div>{artifact.tags.slice(0, 2).map((tag) => <span className="tag" key={tag}>{tag}</span>)}</div>
                      <time>{relativeTime(artifact.createdAt)}</time>
                    </div>
                  </button>
                ))}
                {!artifacts.length && (
                  <div className="empty-list">
                    <UploadCloud size={22} />
                    <strong>{query ? '没有匹配结果' : '这里还没有 artifact'}</strong>
                    <span>{query ? '试试其他关键词' : '支持 HTML 与 Markdown，最大 10 MB'}</span>
                    {!query && selectedCollection && <button onClick={() => setModal('artifact')}>上传第一个</button>}
                  </div>
                )}
              </div>
            </>
          )}
        </section>
      )}

      <main className="preview-panel">
        {selectedArtifact ? (
          <>
            <header className="preview-header">
              <div className="preview-title">
                <div className="breadcrumb"><span>{selectedCollection?.name}</span><ChevronRight size={12} /><span>{selectedArtifact.type === 'html' ? 'HTML' : 'Markdown'}</span></div>
                <h2>{selectedArtifact.title}</h2>
              </div>
              <div className="preview-actions">
                <span className="immutable-badge"><LockKeyhole size={12} /> Immutable</span>
                <button className="ghost-button mobile-details" onClick={() => setDetailsOpen(true)}><Info size={15} /> Details</button>
                <button className="ghost-button" onClick={() => setFullscreen((value) => !value)}><Maximize2 size={15} />{fullscreen ? '退出' : '全屏'}</button>
                <button className="ghost-button danger" onClick={removeArtifact}><Trash2 size={15} /></button>
              </div>
            </header>
            <div className="preview-stage">
              {selectedArtifact.type === 'html' ? (
                <div className="browser-frame">
                  <div className="browser-bar"><i /><i /><i /><span>{selectedArtifact.originalFilename}</span><MoreHorizontal size={15} /></div>
                  <iframe title={selectedArtifact.title} src={selectedArtifact.contentUrl} sandbox="allow-scripts allow-forms" />
                </div>
              ) : (
                <article className="markdown-body">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{markdown}</ReactMarkdown>
                </article>
              )}
            </div>
          </>
        ) : (
          <div className="empty-preview">
            <div className="empty-preview-mark"><Code2 size={26} /></div>
            <h2>把成品放在一个稳定的地方</h2>
            <p>选择一个 artifact 查看内容，或上传一份新的 HTML / Markdown 文件。</p>
            {selectedCollection && <button className="primary-button" onClick={() => setModal('artifact')}><UploadCloud size={16} /> 上传 Artifact</button>}
          </div>
        )}
      </main>

      {selectedArtifact && !fullscreen && (
        <DetailsPanel artifact={selectedArtifact} open={detailsOpen} copied={copied} onClose={() => setDetailsOpen(false)} onCopy={copyLink} />
      )}

      {modal === 'collection' && <CollectionModal onClose={() => setModal(null)} onCreated={afterCollectionCreated} />}
      {modal === 'artifact' && selectedCollection && <ArtifactModal collection={selectedCollection} onClose={() => setModal(null)} onCreated={afterArtifactCreated} />}
      {error && <div className="toast error"><span>{error}</span><button onClick={() => setError(null)}><X size={14} /></button></div>}
    </div>
  )
}

function DetailsPanel({ artifact, open, copied, onClose, onCopy }: { artifact: Artifact; open: boolean; copied: boolean; onClose: () => void; onCopy: () => void }) {
  const metadata = useMemo(() => Object.entries(artifact.metadata ?? {}), [artifact.metadata])
  return (
    <aside className={`details-panel ${open ? 'open' : ''}`}>
      <div className="details-heading"><div><p className="eyebrow">Artifact details</p><h3>元数据</h3></div><button onClick={onClose}><X size={16} /></button></div>
      <section className="details-section">
        <Detail label="File" value={artifact.originalFilename} mono />
        <Detail label="Format" value={`${artifact.type.toUpperCase()} · ${artifact.mediaType}`} />
        <Detail label="Size" value={formatBytes(artifact.sizeBytes)} />
        <Detail label="Created" value={new Date(artifact.createdAt).toLocaleString('zh-CN')} />
      </section>
      <section className="details-section">
        <div className="detail-label">Tags</div>
        <div className="tag-cloud">{artifact.tags.length ? artifact.tags.map((tag) => <span className="tag" key={tag}>{tag}</span>) : <span className="muted">没有标签</span>}</div>
      </section>
      <section className="details-section checksum">
        <div className="detail-label"><Hash size={12} /> SHA-256</div>
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
        <div className="detail-label">Stable public URL</div>
        <button onClick={onCopy}><span>{artifact.publicUrl.replace(/^https?:\/\//, '')}</span>{copied ? <Check size={14} /> : <Clipboard size={14} />}</button>
        <p><LockKeyhole size={11} /> 只要 artifact 存在，这个地址就不会变。</p>
      </section>
    </aside>
  )
}

function Detail({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="detail-row"><span>{label}</span><strong className={mono ? 'mono' : ''}>{value}</strong></div>
}

function CollapseButton({ label, onClick }: { label: string; onClick: () => void }) {
  return <button className="collapse-button" type="button" aria-label={label} title={label} onClick={onClick}><PanelLeftClose size={15} /></button>
}

function SidebarRail({ label, onExpand }: { label: string; onExpand: () => void }) {
  return <button className="sidebar-rail" type="button" aria-label={`展开 ${label} 侧边栏`} title={`展开 ${label} 侧边栏`} onClick={onExpand}><PanelLeftOpen size={16} /><span>{label}</span></button>
}

function CollectionModal({ onClose, onCreated }: { onClose: () => void; onCreated: (collection: Collection) => void }) {
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
    <ModalFrame title="新建 Collection" subtitle="把一组相关的 artifact 收在一起。" onClose={onClose}>
      <form onSubmit={submit}>
        <Field label="名称"><input autoFocus required maxLength={120} value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：产品发布" /></Field>
        <Field label="描述"><textarea rows={3} value={description} onChange={(event) => setDescription(event.target.value)} placeholder="这一组内容是关于什么的？" /></Field>
        <Field label="标识颜色"><div className="color-picker">{colors.map((item) => <button type="button" aria-label={item} className={color === item ? 'selected' : ''} style={{ background: item }} onClick={() => setColor(item)} key={item} />)}</div></Field>
        {error && <p className="form-error">{error}</p>}
        <ModalActions saving={saving} onClose={onClose} action="创建 Collection" />
      </form>
    </ModalFrame>
  )
}

function ArtifactModal({ collection, onClose, onCreated }: { collection: Collection; onClose: () => void; onCreated: (artifact: Artifact) => void }) {
  const [file, setFile] = useState<File | null>(null)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [tags, setTags] = useState('')
  const [metadata, setMetadata] = useState('{}')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const pickFile = (next: File | null) => {
    if (!next) return
    setFile(next)
    if (!title) setTitle(next.name.replace(/\.(html?|md|markdown)$/i, ''))
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
    <ModalFrame title="上传 Artifact" subtitle={`保存到 ${collection.name}。发布后内容与元数据不可修改。`} onClose={onClose} wide>
      <form onSubmit={submit}>
        <input ref={inputRef} className="visually-hidden" type="file" accept=".html,.htm,.md,.markdown,text/html,text/markdown" onChange={(event) => pickFile(event.target.files?.[0] ?? null)} />
        <button type="button" className={`drop-zone ${file ? 'has-file' : ''}`} onClick={() => inputRef.current?.click()} onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); pickFile(event.dataTransfer.files[0] ?? null) }}>
          {file ? <><span className={`upload-file-icon ${file.name.match(/\.html?$/i) ? 'html' : 'markdown'}`}>{file.name.match(/\.html?$/i) ? <FileCode2 size={20} /> : <FileText size={20} />}</span><div><strong>{file.name}</strong><span>{formatBytes(file.size)} · 点击更换</span></div><Check size={18} className="upload-check" /></> : <><UploadCloud size={24} /><strong>拖入 HTML 或 Markdown 文件</strong><span>或点击浏览 · 最大 10 MB</span></>}
        </button>
        <div className="form-grid">
          <Field label="标题"><input required maxLength={200} value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Artifact 标题" /></Field>
          <Field label="标签"><input value={tags} onChange={(event) => setTags(event.target.value)} placeholder="release, docs, v1" /></Field>
        </div>
        <Field label="描述"><input value={description} onChange={(event) => setDescription(event.target.value)} placeholder="一句话说明这个 artifact" /></Field>
        <Field label="自定义元数据（JSON）"><textarea className="code-input" rows={4} value={metadata} onChange={(event) => setMetadata(event.target.value)} spellCheck={false} /></Field>
        <div className="immutable-callout"><LockKeyhole size={15} /><div><strong>Immutable by design</strong><span>上传后不可覆盖或编辑；如需修改，请创建新的 artifact。</span></div></div>
        {error && <p className="form-error">{error}</p>}
        <ModalActions saving={saving} onClose={onClose} action="上传并发布" />
      </form>
    </ModalFrame>
  )
}

function ModalFrame({ title, subtitle, onClose, wide, children }: { title: string; subtitle: string; onClose: () => void; wide?: boolean; children: React.ReactNode }) {
  return <div className="modal-backdrop" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><div className={`modal ${wide ? 'wide' : ''}`}><header><div><h2>{title}</h2><p>{subtitle}</p></div><button onClick={onClose}><X size={17} /></button></header>{children}</div></div>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="field"><span>{label}</span>{children}</label>
}

function ModalActions({ saving, onClose, action }: { saving: boolean; onClose: () => void; action: string }) {
  return <div className="modal-actions"><button type="button" className="ghost-button" onClick={onClose}>取消</button><button className="primary-button" disabled={saving}>{saving ? '处理中…' : action}</button></div>
}

function LoadingScreen() {
  return <div className="loading-screen"><div className="brand-mark"><Box size={18} /></div><span>Loading Artifact Hub…</span></div>
}

function relativeTime(iso: string) {
  const seconds = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (seconds < 60) return '刚刚'
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`
  if (seconds < 86400 * 30) return `${Math.floor(seconds / 86400)} 天前`
  return new Date(iso).toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 ** 2).toFixed(1)} MB`
}

export default App
