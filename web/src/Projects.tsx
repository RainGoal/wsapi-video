import { t } from "./i18n";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, errorMessage } from "./api";
import { PageHeading, useStudio } from "./App";
import { Pagination, useDebouncedValue, usePagedResource } from "./pagination";
import type { Project } from "./types";
import { Empty, Icon, LoadingError, Modal, Notice, formatDate } from "./ui";
export function Projects() {
  const { navigate, notify } = useStudio();
  const [search, setSearch] = useState(""),
    [archived, setArchived] = useState(false),
    [create, setCreate] = useState(false),
    [editing, setEditing] = useState<Project | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [deleteProject, setDeleteProject] = useState<Project | null>(null);
  const busyRef = useRef(false),
    live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  const q = useDebouncedValue(search);
  const projects = usePagedResource<Project>("/projects", {
    status: archived ? "archived" : "active",
    q,
  });
  const list = projects.data.items;
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busyRef.current || !live.current) return;
    busyRef.current = true;
    setBusy(true);
    setError("");
    try {
      const form = new FormData(event.currentTarget);
      const project = await api<Project>(
        editing ? `/projects/${editing.id}` : "/projects",
        editing ? "PATCH" : "POST",
        {
          name: form.get("name"),
          description: form.get("description"),
          ...(editing ? { revision: editing.revision } : {}),
        },
      );
      if (!live.current) return;
      setCreate(false);
      setEditing(null);
      projects.refresh();
      if (!editing) navigate("project", project.id);
      else notify(t("项目资料已更新"));
    } catch (reason) {
      if (live.current) setError(errorMessage(reason));
    } finally {
      busyRef.current = false;
      if (live.current) setBusy(false);
    }
  }
  function closeEditor() {
    if (busyRef.current) return;
    setCreate(false);
    setEditing(null);
  }
  function closeDelete() {
    if (!busyRef.current) setDeleteProject(null);
  }
  async function archive(project: Project) {
    try {
      await api(`/projects/${project.id}`, "PATCH", {
        revision: project.revision,
        status: project.status === "archived" ? "active" : "archived",
      });
      projects.refresh();
      notify(
        project.status === "archived"
          ? t("项目已恢复")
          : t("项目已归档，已有任务与作品仍会保留"),
      );
    } catch (reason) {
      notify(errorMessage(reason), true);
    }
  }
  async function remove() {
    if (!deleteProject || busyRef.current || !live.current) return;
    busyRef.current = true;
    setBusy(true);
    try {
      const target =
        deleteProject.status === "archived"
          ? deleteProject
          : await api<Project>(`/projects/${deleteProject.id}`, "PATCH", {
              revision: deleteProject.revision,
              status: "archived",
            });
      if (!live.current) return;
      setDeleteProject(target);
      if (target !== deleteProject) projects.refresh();
      await api(`/projects/${target.id}`, "DELETE", {
        revision: target.revision,
      });
      if (!live.current) return;
      setDeleteProject(null);
      projects.refresh();
      notify(t("项目已删除"));
    } catch (reason) {
      if (live.current) notify(errorMessage(reason), true);
    } finally {
      busyRef.current = false;
      if (live.current) setBusy(false);
    }
  }
  return (
    <>
      <PageHeading
        eyebrow="THE CREATIVE WORKSPACE"
        title={t("留住想法，开始创作")}
        description={t("把灵感展开，把每一次探索留在项目里。")}
      >
        <button
          className="button primary"
          onClick={() => {
            setCreate(true);
            setError("");
          }}
        >
          <Icon name="plus" />
          {t("新建项目")}
        </button>
      </PageHeading>
      <div className="project-banner">
        <div>
          <span className="eyebrow">FROM AN IDEA TO THE FINAL FRAME</span>
          <h2>{t("让创作，沿着你的思路生长。")}</h2>
          <p>{t("画布探索图片与视频 · 连线组织创作关系 · 素材沉淀每个版本")}</p>
        </div>
        <div className="banner-diagram" aria-hidden="true">
          <span>
            <Icon name="image" size={24} />
          </span>
          <i />
          <span>
            <Icon name="canvas" size={26} />
          </span>
          <i />
          <span>
            <Icon name="video" size={24} />
          </span>
        </div>
      </div>
      <div className="collection-toolbar">
        <div className="tab-list">
          <button
            className={!archived ? "active" : ""}
            onClick={() => setArchived(false)}
          >
            {t("全部项目")}{" "}
            <small>
              {!archived && !projects.loading && !projects.error
                ? projects.total
                : ""}
            </small>
          </button>
          <button
            className={archived ? "active" : ""}
            onClick={() => setArchived(true)}
          >
            {t("已归档")}
          </button>
        </div>
        <div className="search-input">
          <Icon name="search" size={16} />
          <input
            aria-label={t("搜索项目")}
            placeholder={t("搜索项目名称或描述")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
      </div>
      <LoadingError
        loading={projects.loading}
        error={projects.error}
        retry={projects.refresh}
      />
      {!projects.loading &&
        !projects.error &&
        (list.length ? (
          <div className="project-grid">
            {list.map((project, index) => (
              <article
                className="project-card"
                key={project.id}
                style={{ animationDelay: `${index * 40}ms` }}
              >
                <button
                  className="project-cover"
                  onClick={() => navigate("project", project.id)}
                  aria-label={t("打开 {0}", project.name)}
                >
                  {project.cover_asset_id ? (
                    <img
                      src={`/api/v1/assets/${project.cover_asset_id}/content`}
                      alt=""
                    />
                  ) : (
                    <div className="project-cover-art">
                      <span className="cover-cross top-left" />
                      <span className="cover-cross bottom-right" />
                      <Icon name="canvas" size={38} />
                      <span className="cover-word">WORK IN PROGRESS</span>
                    </div>
                  )}
                  <span className="project-open">
                    {t("打开项目")}
                    <Icon name="arrow" size={16} />
                  </span>
                </button>
                <div className="project-card-body">
                  <div className="project-card-title">
                    <button onClick={() => navigate("project", project.id)}>
                      {project.name}
                    </button>
                    <details className="action-menu">
                      <summary aria-label={t("项目操作")}>
                        <Icon name="more" />
                      </summary>
                      <div>
                        <button
                          onClick={() => {
                            setEditing(project);
                            setError("");
                          }}
                        >
                          {t("编辑信息")}
                        </button>
                        <button onClick={() => void archive(project)}>
                          {archived ? t("恢复项目") : t("归档项目")}
                        </button>
                        <button
                          className="danger-text"
                          onClick={() => setDeleteProject(project)}
                        >
                          {t("删除项目")}
                        </button>
                      </div>
                    </details>
                  </div>
                  <p>{project.description || t("等待下一个好想法")}</p>
                  <footer>
                    <span>
                      <Icon name="clock" size={12} />
                      {formatDate(project.updated_at)}
                    </span>
                    <span>
                      PROJECT{" "}
                      <span className="mono">
                        {project.id.slice(-4).toUpperCase()}
                      </span>
                    </span>
                  </footer>
                </div>
              </article>
            ))}
            {!archived && !search && (
              <button
                className="new-project-card"
                onClick={() => {
                  setCreate(true);
                  setError("");
                }}
              >
                <span>
                  <Icon name="plus" size={26} />
                </span>
                <strong>{t("下一个好故事")}</strong>
                <small>{t("从一个空白项目开始")}</small>
              </button>
            )}
          </div>
        ) : (
          <Empty
            icon="canvas"
            title={
              search
                ? t("没有找到这个项目")
                : archived
                  ? t("还没有归档项目")
                  : t("你的创作空间，准备就绪")
            }
            description={
              search
                ? t("试试其他名称，或清空搜索条件。")
                : archived
                  ? t("归档项目会保留素材、任务和作品。")
                  : t(
                      "新建项目，在画布上放入图片或视频，用连线串起你的创作思路。",
                    )
            }
            action={
              !archived &&
              !search && (
                <button
                  className="button primary"
                  onClick={() => setCreate(true)}
                >
                  <Icon name="plus" />
                  {t("创建第一个项目")}
                </button>
              )
            }
          />
        ))}
      {!projects.error && (
        <Pagination
          page={projects.page}
          size={projects.size}
          total={projects.total}
          loading={projects.loading}
          onPage={projects.setPage}
        />
      )}
      <div className="page-footnote">
        <span>YOUR NEXT STORY STARTS HERE.</span>
        <span>{t("每一次探索，都值得留下。")}</span>
      </div>
      {(create || editing) && (
        <Modal
          title={editing ? t("编辑项目") : t("创建一个新项目")}
          onClose={closeEditor}
          closeDisabled={busy}
        >
          <form onSubmit={save} aria-busy={busy}>
            {error && <Notice error>{error}</Notice>}
            <label>
              {t("项目名称")}
              <input
                name="name"
                placeholder={t("例如：秋日香氛 · 品牌短片")}
                defaultValue={editing?.name}
                required
                autoFocus
                maxLength={120}
                disabled={busy}
              />
            </label>
            <label>
              {t("项目描述")}
              <small>{t("选填")}</small>
              <textarea
                name="description"
                placeholder={t("记录创作目标、视觉方向或交付要求…")}
                defaultValue={editing?.description}
                maxLength={2000}
                rows={3}
                disabled={busy}
              />
            </label>
            <div className="modal-actions">
              <button
                type="button"
                className="button secondary"
                onClick={closeEditor}
                disabled={busy}
              >
                {t("取消")}
              </button>
              <button className="button primary" disabled={busy}>
                {busy
                  ? t("正在保存…")
                  : editing
                    ? t("保存修改")
                    : t("创建并进入工作台")}
                <Icon name="arrow" size={16} />
              </button>
            </div>
          </form>
        </Modal>
      )}
      {deleteProject && (
        <Modal title={t("删除项目")} onClose={closeDelete} closeDisabled={busy}>
          <p className="modal-copy">
            {t("确认删除「")}
            {deleteProject.name}
            {t("」？此操作会从项目空间移除项目。已有任务不因此取消或退款。")}
          </p>
          <div className="modal-actions">
            <button
              className="button secondary"
              onClick={closeDelete}
              disabled={busy}
            >
              {t("保留项目")}
            </button>
            <button
              className="button danger"
              onClick={() => void remove()}
              disabled={busy}
            >
              {t("确认删除")}
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}
