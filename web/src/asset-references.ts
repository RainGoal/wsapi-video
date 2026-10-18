import type { PageResult } from "./paging";

export type AssetReference = {
  project_id: string;
  project_name: string;
  project_status: string;
  project_deleted: boolean;
  owner_type: "canvas" | "shot" | "task" | "cover";
  owner_id: string;
  owner_label: string;
};
export type WorkspaceView = "canvas" | "assets";

export function workspaceLocation(query = ""): {
  view: WorkspaceView | undefined;
} {
  const params = new URLSearchParams(query);
  const view = params.getAll("view");
  if (view.length > 1) return { view: undefined };
  if (view[0] === "shots" || params.has("shot") || params.has("shot_id"))
    return { view: "canvas" };
  return {
    view:
      view.length === 1 && ["canvas", "assets"].includes(view[0])
        ? (view[0] as WorkspaceView)
        : undefined,
  };
}

export function assetReferenceTarget(reference: AssetReference) {
  if (reference.owner_type === "task")
    return { page: "tasks", id: encodeURIComponent(reference.owner_id) };
  if (reference.project_deleted) return null;
  const params = new URLSearchParams({
    view: reference.owner_type === "cover" ? "assets" : "canvas",
  });
  return {
    page: "project",
    id: `${encodeURIComponent(reference.project_id)}?${params}`,
  };
}

export function validReferencePage(
  value: PageResult<AssetReference>,
  offset: number,
  limit: number,
): boolean {
  return (
    Boolean(value) &&
    Array.isArray(value.items) &&
    Number.isSafeInteger(value.total) &&
    value.total >= 0 &&
    value.offset === offset &&
    value.limit === limit &&
    value.items.length === Math.min(limit, Math.max(0, value.total - offset)) &&
    value.items.every(
      (item) =>
        item &&
        typeof item.project_id === "string" &&
        Boolean(item.project_id) &&
        typeof item.owner_id === "string" &&
        Boolean(item.owner_id) &&
        typeof item.project_name === "string" &&
        typeof item.project_status === "string" &&
        typeof item.project_deleted === "boolean" &&
        typeof item.owner_label === "string" &&
        ["canvas", "shot", "task", "cover"].includes(item.owner_type),
    ) &&
    new Set(
      value.items.map((item) =>
        JSON.stringify([item.project_id, item.owner_type, item.owner_id]),
      ),
    ).size === value.items.length
  );
}
