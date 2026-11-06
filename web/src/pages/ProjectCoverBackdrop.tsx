import './project-cover-backdrop.css'

export function ProjectCoverBackdrop({ url }: { url: string }) {
  return <div className="project-cover-backdrop" aria-hidden="true">
    <div className="project-cover-backdrop-frame">
      <img key={url} src={url} alt="" onError={(event) => { event.currentTarget.hidden = true }} />
    </div>
  </div>
}
