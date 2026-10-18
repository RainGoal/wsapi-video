export type Position = { x: number; y: number };
export type ViewportTransform = Position & { k: number };
export enum CanvasNodeType {
  Image = "image",
  Text = "text",
  Config = "config",
  Video = "video",
  Audio = "audio",
  Group = "group",
}
export type CanvasNodeData = {
  id: string;
  type: CanvasNodeType;
  title: string;
  position: Position;
  width: number;
  height: number;
  metadata?: {
    groupId?: string;
    content?: string;
    prompt?: string;
    assetId?: string;
    shotId?: string;
    productId?: string;
    [key: string]: unknown;
  };
};
export type CanvasConnection = {
  id: string;
  fromNodeId: string;
  toNodeId: string;
};
export type ConnectionHandle = {
  nodeId: string;
  handleType: "source" | "target";
};
export type Graph = {
  nodes: CanvasNodeData[];
  connections: CanvasConnection[];
  viewport: ViewportTransform;
};
export const emptyGraph = (): Graph => ({
  nodes: [],
  connections: [],
  viewport: { x: 80, y: 80, k: 1 },
});
