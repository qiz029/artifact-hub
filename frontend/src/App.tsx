import { useCallback, useEffect, useState } from 'react'
import {
  ArrowUpRight,
  ArrowLeft,
  Box,
  Braces,
  Check,
  ChevronRight,
  Code2,
  Copy,
  Database,
  FileCode2,
  FileText,
  FolderPlus,
  Info,
  Layers3,
  LockKeyhole,
  Maximize2,
  MoreHorizontal,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  Search,
  ShieldCheck,
  Table2,
  UploadCloud,
  X,
} from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api } from './api'
import { ArtifactDetails } from './components/ArtifactDetails'
import { ArtifactModal, CollectionModal, DeleteArtifactModal } from './components/ArtifactModals'
import { relativeTime } from './lib/format'
import type { Artifact, Collection } from './types'

type Modal = 'collection' | 'artifact' | null
type MobileStage = 'collections' | 'artifacts' | 'detail'

function artifactTypeLabel(type: Artifact['type'], long = false) {
  if (type === 'markdown') return long ? 'Markdown' : 'MD'
  return type.toUpperCase()
}

function ArtifactTypeIcon({ type, size = 15 }: { type: Artifact['type']; size?: number }) {
  if (type === 'html') return <FileCode2 size={size} />
  if (type === 'json') return <Braces size={size} />
  if (type === 'csv') return <Table2 size={size} />
  return <FileText size={size} />
}

function App() {
  const [collections, setCollections] = useState<Collection[]>([])
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [collectionId, setCollectionId] = useState<string | null>(null)
  const [artifactId, setArtifactId] = useState<string | null>(null)
  const [markdown, setMarkdown] = useState('')
  const [collectionQuery, setCollectionQuery] = useState('')
  const [artifactQuery, setArtifactQuery] = useState('')
  const [modal, setModal] = useState<Modal>(null)
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [collectionsCollapsed, setCollectionsCollapsed] = useState(false)
  const [artifactsCollapsed, setArtifactsCollapsed] = useState(false)
  const [mobileStage, setMobileStage] = useState<MobileStage>('collections')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const selectedCollection = collections.find((item) => item.id === collectionId) ?? null
  const selectedArtifact = artifacts.find((item) => item.id === artifactId) ?? null
  const visibleCollections = collectionQuery
    ? collections.filter((item) => `${item.name} ${item.description}`.toLowerCase().includes(collectionQuery.toLowerCase()))
    : collections
  const totalArtifacts = collections.reduce((total, collection) => total + collection.artifactCount, 0)

  const refreshCollections = useCallback(async () => {
    const next = await api.collections()
    setCollections(next)
    setCollectionId((current) => current && next.some((item) => item.id === current) ? current : next[0]?.id ?? null)
  }, [])

  useEffect(() => {
    refreshCollections().catch((reason) => setError(reason instanceof Error ? reason.message : '无法读取 Collection')).finally(() => setLoading(false))
  }, [refreshCollections])

  useEffect(() => {
    if (!collectionId) {
      setArtifacts([])
      setArtifactId(null)
      return
    }
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      api.artifacts(collectionId, artifactQuery, controller.signal)
        .then((next) => {
          setArtifacts(next)
          setArtifactId((current) => current && next.some((item) => item.id === current) ? current : next[0]?.id ?? null)
        })
        .catch((reason) => {
          if (reason instanceof DOMException && reason.name === 'AbortError') return
          setError(reason instanceof Error ? reason.message : '无法读取 Artifact')
        })
    }, artifactQuery ? 180 : 0)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [collectionId, artifactQuery])

  useEffect(() => {
    if (selectedArtifact?.type !== 'markdown') {
      setMarkdown('')
      return
    }
    const controller = new AbortController()
    api.artifactContent(selectedArtifact.id, controller.signal).then(setMarkdown).catch((reason) => {
      if (reason instanceof DOMException && reason.name === 'AbortError') return
      setError(reason instanceof Error ? reason.message : '无法读取 Markdown 内容')
    })
    return () => controller.abort()
  }, [selectedArtifact?.id, selectedArtifact?.type])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const focusVisibleSearch = (...selectors: string[]) => {
        selectors
          .map((selector) => document.querySelector<HTMLInputElement>(selector))
          .find((input) => input && input.getClientRects().length > 0)
          ?.focus()
      }
      if (event.key === 'Escape') {
        setModal(null)
        setDeleteConfirmOpen(false)
        setFullscreen(false)
        setDetailsOpen(false)
      }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        focusVisibleSearch('#collection-search', '#artifact-search')
      }
      const target = event.target as HTMLElement | null
      const isTyping = target?.matches('input, textarea, [contenteditable="true"]')
      if (event.key === '/' && !isTyping && !event.metaKey && !event.ctrlKey && !event.altKey) {
        event.preventDefault()
        focusVisibleSearch('#artifact-search', '#collection-search')
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  const selectCollection = (id: string) => {
    const isNewCollection = id !== collectionId
    setCollectionId(id)
    if (isNewCollection) {
      setArtifacts([])
      setArtifactId(null)
    }
    setCollectionQuery('')
    setArtifactQuery('')
    setDetailsOpen(false)
    setMobileStage('artifacts')
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
    setMobileStage('detail')
  }

  const removeArtifact = async () => {
    if (!selectedArtifact) return false
    try {
      await api.deleteArtifact(selectedArtifact.id)
      const next = await api.artifacts(selectedArtifact.collectionId, artifactQuery)
      setArtifacts(next)
      setArtifactId(next[0]?.id ?? null)
      await refreshCollections()
      setMobileStage('artifacts')
      setDeleteConfirmOpen(false)
      return true
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '删除失败')
      return false
    }
  }

  const copyLink = async () => {
    if (!selectedArtifact) return
    try {
      await navigator.clipboard.writeText(selectedArtifact.publicUrl)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      setError('无法复制链接，请从元数据面板中手动复制')
    }
  }

  const mobileBack = () => {
    setDetailsOpen(false)
    setMobileStage((stage) => stage === 'detail' ? 'artifacts' : 'collections')
  }

  const mobileTitle = mobileStage === 'collections'
    ? 'Artifact Hub'
    : mobileStage === 'artifacts'
      ? selectedCollection?.name ?? 'Artifacts'
      : selectedArtifact?.title ?? 'Artifact'

  if (loading) return <LoadingScreen />

  return (
    <div className={`app-shell mobile-stage-${mobileStage} ${fullscreen ? 'is-fullscreen' : ''} ${collectionsCollapsed ? 'collections-collapsed' : ''} ${artifactsCollapsed ? 'artifacts-collapsed' : ''}`}>
      {!fullscreen && (
        <header className="mobile-header">
          {mobileStage !== 'collections' ? (
            <button className="mobile-back" type="button" aria-label="返回" onClick={mobileBack}><ArrowLeft size={19} /></button>
          ) : <div className="mobile-brand-mark"><Box size={15} /></div>}
          <h1>{mobileTitle}</h1>
          {mobileStage === 'collections' && <button className="mobile-primary-action" type="button" aria-label="新建 Collection" onClick={() => setModal('collection')}><Plus size={19} /></button>}
          {mobileStage === 'artifacts' && selectedCollection && <button className="mobile-primary-action" type="button" aria-label="上传 Artifact" onClick={() => setModal('artifact')}><Plus size={19} /></button>}
          {mobileStage === 'detail' && selectedArtifact && <button className="mobile-primary-action link-action" type="button" aria-label={copied ? '稳定链接已复制' : '复制稳定链接'} onClick={copyLink}>{copied ? <Check size={18} /> : <Copy size={18} />}</button>}
        </header>
      )}
      {!fullscreen && (
        <aside className="collections-panel">
          {collectionsCollapsed ? (
            <SidebarRail label="Collections" onExpand={() => setCollectionsCollapsed(false)} />
          ) : (
            <>
              <div className="brand">
                <div className="brand-mark"><Box size={14} strokeWidth={2.4} /></div>
                <div className="brand-copy"><strong>Artifact Hub</strong><span>Permanent host</span></div>
                <CollapseButton label="收起 Collection 侧边栏" onClick={() => setCollectionsCollapsed(true)} />
              </div>
              <div className="global-search">
                <Search size={14} />
                <input id="collection-search" value={collectionQuery} onChange={(event) => setCollectionQuery(event.target.value)} placeholder="搜索 Collection" />
                <kbd>⌘K</kbd>
              </div>
              <div className="library-card">
                <div className="library-card-glow" />
                <div className="library-kicker"><Database size={12} /> Permanent library</div>
                <strong>{totalArtifacts}</strong>
                <span>artifacts hosted across {collections.length} {collections.length === 1 ? 'collection' : 'collections'}</span>
                <div className="library-signal"><i /> Postgres-backed · content addressed</div>
              </div>
              <div className="panel-label"><span>Collections</span><span>{collections.length}</span></div>
              <nav className="collections-list">
                {visibleCollections.map((collection) => (
                  <button className={collection.id === collectionId ? 'collection active' : 'collection'} onClick={() => selectCollection(collection.id)} key={collection.id}>
                    <span className="collection-dot" style={{ background: collection.color }} />
                    <span className="collection-copy"><span className="collection-name">{collection.name}</span><span className="collection-description">{collection.description || '暂无描述'}</span></span>
                    <span className="collection-count">{collection.artifactCount}</span>
                    <ChevronRight size={13} className="collection-chevron" />
                  </button>
                ))}
                <button className="new-collection" onClick={() => setModal('collection')}><FolderPlus size={14} /> 新建 Collection</button>
              </nav>
              <div className="storage-note"><ShieldCheck size={14} /><span>内容与指纹不可变</span></div>
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
                  <p>{selectedCollection?.description || '创建一个 Collection，开始发布可长期引用的工作。'}</p>
                  {selectedCollection && <div className="collection-status"><i /><span>{selectedCollection.artifactCount} published</span></div>}
                </div>
                <div className="artifact-header-actions">
                  {selectedCollection && <button className="icon-primary" aria-label="上传 artifact" onClick={() => setModal('artifact')}><Plus size={16} /></button>}
                  <CollapseButton label="收起 Artifact 侧边栏" onClick={() => setArtifactsCollapsed(true)} />
                </div>
              </header>
              <div className="artifact-search">
                <Search size={15} />
                <input id="artifact-search" value={artifactQuery} onChange={(event) => setArtifactQuery(event.target.value)} placeholder={`在 ${selectedCollection?.name ?? 'Collection'} 中搜索`} />
                <kbd>/</kbd>
              </div>
              <div className="artifact-list">
                {artifacts.map((artifact) => (
                  <button className={artifact.id === artifactId ? 'artifact-row active' : 'artifact-row'} key={artifact.id} onClick={() => { setArtifactId(artifact.id); setDetailsOpen(false); setMobileStage('detail') }}>
                    <div className="artifact-row-top">
                      <span className={`file-icon ${artifact.type}`}><ArtifactTypeIcon type={artifact.type} /></span>
                      <strong>{artifact.title}</strong>
                      <span className="type-pill">{artifactTypeLabel(artifact.type)}</span>
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
                    <strong>{artifactQuery ? '没有匹配结果' : '这里还没有 Artifact'}</strong>
                    <span>{artifactQuery ? '试试其他关键词' : '发布 HTML、Markdown、JSON 或 CSV，获得一个稳定地址'}</span>
                    {!artifactQuery && selectedCollection && <button onClick={() => setModal('artifact')}>发布第一个</button>}
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
                <div className="breadcrumb"><span>{selectedCollection?.name}</span><ChevronRight size={12} /><span>{artifactTypeLabel(selectedArtifact.type, true)}</span></div>
                <h2>{selectedArtifact.title}</h2>
                {selectedArtifact.description && <p>{selectedArtifact.description}</p>}
              </div>
              <div className="preview-actions">
                <span className="published-badge"><i /> Published</span>
                <button className="ghost-button copy-link-button" type="button" onClick={copyLink}>{copied ? <Check size={15} /> : <Copy size={15} />}{copied ? '已复制' : '复制链接'}</button>
                <a className="ghost-button open-link-button" href={selectedArtifact.publicUrl} target="_blank" rel="noreferrer"><ArrowUpRight size={15} /><span>打开</span></a>
                <button className="ghost-button mobile-details" type="button" onClick={() => setDetailsOpen(true)}><Info size={15} /> 元数据</button>
                <button className="ghost-button" type="button" onClick={() => setFullscreen((value) => !value)}><Maximize2 size={15} />{fullscreen ? '退出' : '全屏'}</button>
              </div>
            </header>
            <div className="mobile-artifact-meta">
              <div className="mobile-meta-row">
                <span className="type-pill">{artifactTypeLabel(selectedArtifact.type, true).toUpperCase()}</span>
                <time>{relativeTime(selectedArtifact.createdAt)}</time>
                <span className="mobile-immutable"><LockKeyhole size={11} /> Immutable</span>
                <button type="button" onClick={() => setDetailsOpen(true)}><Info size={15} /> 元数据</button>
              </div>
              {!!selectedArtifact.tags.length && <div className="mobile-tag-row">{selectedArtifact.tags.map((tag) => <span className="tag" key={tag}>{tag}</span>)}</div>}
            </div>
            <div className="preview-stage">
              {selectedArtifact.type !== 'markdown' ? (
                <div className="browser-frame">
                  <div className="browser-bar"><i /><i /><i /><span>{selectedArtifact.publicUrl.replace(/^https?:\/\//, '')}</span><MoreHorizontal size={15} /></div>
                  <iframe
                    title={selectedArtifact.title}
                    src={selectedArtifact.type === 'html' ? selectedArtifact.contentUrl : selectedArtifact.publicUrl}
                    sandbox={selectedArtifact.type === 'html' ? 'allow-scripts allow-forms' : ''}
                  />
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
            <div className="empty-preview-orbit"><div className="empty-preview-mark"><Layers3 size={25} /></div><i /><i /><i /></div>
            <span className="empty-preview-kicker">Content-addressed workspace</span>
            <h2>Publish once. Reference forever.</h2>
            <p>把 HTML、Markdown 与结构化数据变成稳定、可验证、可分享的 Artifact。</p>
            <div className="empty-preview-features"><span><LockKeyhole size={13} /> Immutable</span><span><Code2 size={13} /> Sandboxed</span><span><Database size={13} /> Self-hosted</span></div>
            {selectedCollection && <button className="primary-button" onClick={() => setModal('artifact')}><UploadCloud size={16} /> 发布 Artifact</button>}
          </div>
        )}
      </main>

      {selectedArtifact && !fullscreen && detailsOpen && <button className="details-scrim" type="button" aria-label="关闭元数据" onClick={() => setDetailsOpen(false)} />}
      {selectedArtifact && !fullscreen && <ArtifactDetails artifact={selectedArtifact} open={detailsOpen} copied={copied} onClose={() => setDetailsOpen(false)} onCopy={copyLink} onDelete={() => { setDetailsOpen(false); setDeleteConfirmOpen(true) }} />}

      {modal === 'collection' && <CollectionModal onClose={() => setModal(null)} onCreated={afterCollectionCreated} />}
      {modal === 'artifact' && selectedCollection && <ArtifactModal collection={selectedCollection} onClose={() => setModal(null)} onCreated={afterArtifactCreated} />}
      {deleteConfirmOpen && selectedArtifact && <DeleteArtifactModal artifact={selectedArtifact} onClose={() => setDeleteConfirmOpen(false)} onConfirm={removeArtifact} />}
      {error && <div className="toast error"><span>{error}</span><button onClick={() => setError(null)}><X size={14} /></button></div>}
    </div>
  )
}

function CollapseButton({ label, onClick }: { label: string; onClick: () => void }) {
  return <button className="collapse-button" type="button" aria-label={label} title={label} onClick={onClick}><PanelLeftClose size={15} /></button>
}

function SidebarRail({ label, onExpand }: { label: string; onExpand: () => void }) {
  return <button className="sidebar-rail" type="button" aria-label={`展开 ${label} 侧边栏`} title={`展开 ${label} 侧边栏`} onClick={onExpand}><PanelLeftOpen size={16} /><span>{label}</span></button>
}

function LoadingScreen() {
  return <div className="loading-screen"><div className="loading-orbit"><div className="brand-mark"><Box size={18} /></div><i /><i /></div><strong>Artifact Hub</strong><span>Opening your permanent library…</span></div>
}

export default App
