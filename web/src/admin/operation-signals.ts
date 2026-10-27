export type SignalSample = {
  id: string;
  occurred_at: string;
  attempts: number;
};
export type OperationSignal = {
  code: string;
  count: number;
  oldest_at: string | null;
  samples: SignalSample[];
};
export type OperationsSnapshot = {
  generated_at: string;
  review_after_seconds: number;
  sample_limit: number;
  signals: OperationSignal[];
};
type SignalDefinition = {
  title: [string, string];
  description: [string, string];
  section: "tasks" | "orders" | "settings";
  status?: string;
};
export const signalDefinitions: Record<string, SignalDefinition> = {
  task_submission_unknown: {
    title: ["提交待确认", "Unconfirmed submissions"],
    description: [
      "提交结果尚未确认，积分仍冻结。请先核查原任务与上游编号。",
      "The submission is unconfirmed and credits remain held. Inspect the original task and provider ID.",
    ],
    section: "tasks",
    status: "submission_unknown",
  },
  task_frozen_overdue: {
    title: ["冻结积分逾期", "Overdue credit holds"],
    description: [
      "仍有积分冻结，且任务创建时间已超过人工核查阈值。",
      "Credits remain held beyond the review threshold measured from task creation.",
    ],
    section: "tasks",
    status: "frozen_overdue",
  },
  task_archive_failed: {
    title: ["归档异常", "Archival exceptions"],
    description: [
      "生成已完成，归档正在重试、失败或需核查，积分仍冻结。",
      "Generation completed, but archival is retrying, failed or requires review, with credits still held.",
    ],
    section: "tasks",
    status: "archive_failed",
  },
  task_lease_expired: {
    title: ["工作租约过期", "Expired worker leases"],
    description: [
      "本地工作租约已到期，积分仍冻结。请检查任务与工作进程。",
      "The local worker lease expired while credits remain held. Inspect the task and worker.",
    ],
    section: "tasks",
    status: "lease_expired",
  },
  payment_review: {
    title: ["支付待核查", "Payments requiring review"],
    description: [
      "待入账订单已支付、已过期或已有多次核查。请进入订单核对。",
      "Credit-pending orders are paid, expired or have repeated checks. Inspect the order.",
    ],
    section: "orders",
    status: "review_required",
  },
  mail_pending: {
    title: ["邮件待投递", "Pending mail"],
    description: [
      "尚未标记已发送的本地邮件记录，包含重试记录。",
      "Local outbox records not yet marked as sent, including retries.",
    ],
    section: "settings",
  },
  mail_retry: {
    title: ["邮件投递重试", "Mail with delivery attempts"],
    description: [
      "未发送且已有投递尝试的记录，是待投递邮件的子集。",
      "Unsent records with delivery attempts; a subset of pending mail.",
    ],
    section: "settings",
  },
  procurement_review_disputed: {
    title: ["人工采购差异", "Disputed procurement reviews"],
    description: [
      "每个任务最新的人工核对仍有差异，与生成异常处置及零售结算独立。",
      "The latest manual review for each task is disputed, independently of worker exceptions and retail settlement.",
    ],
    section: "tasks",
    status: "procurement_review_disputed",
  },
};

export function signalDefinition(code: string): SignalDefinition | undefined {
  return Object.hasOwn(signalDefinitions, code)
    ? signalDefinitions[code]
    : undefined;
}

export function signalDestination(
  code: string,
  sampleId?: string,
): string | null {
  const definition = signalDefinition(code);
  if (!definition) return null;
  if (definition.section === "settings") return "settings";
  return sampleId
    ? `${definition.section}?record=${encodeURIComponent(sampleId)}`
    : `${definition.section}?status=${definition.status}`;
}

export function recordsSignalTarget(kind: string, query = "") {
  const params = new URLSearchParams(query);
  const ids = params.getAll("record"),
    statuses = params.getAll("status");
  const recordId =
    ["tasks", "orders"].includes(kind) &&
    ids.length === 1 &&
    ids[0].length <= 200
      ? ids[0]
      : "";
  const allowed = Object.values(signalDefinitions)
    .filter((item) => item.section === kind)
    .map((item) => item.status);
  return {
    recordId,
    initialStatus:
      statuses.length === 1 && allowed.includes(statuses[0]) ? statuses[0] : "",
  };
}

export function recordsUserTarget(kind: string, query = ""): string {
  const params = new URLSearchParams(query);
  const users = params.getAll(kind === "audit" ? "target_user_id" : "user_id");
  return ["tasks", "orders", "adjust", "audit"].includes(kind) &&
    users.length === 1 &&
    users[0].length > 0 &&
    users[0].length <= 100
    ? users[0]
    : "";
}
