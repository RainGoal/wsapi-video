import { useState } from "react";
import { useStudio } from "../App";
import { Icon, LoadingError, Status, formatDate, useResource } from "../ui";
import { a } from "./common";
import { SettingsPage } from "./SettingsPage";
import { CatalogPage } from "./CatalogPage";
import { RecordsPage, AdjustmentPage } from "./RecordsPage";
import { UsersPage } from "./UsersPage";
import { OperationsSignals } from "./OperationsSignals";
import { recordsSignalTarget, recordsUserTarget } from "./operation-signals";
import "./admin.css";

export const adminSections = [
  {
    id: "overview",
    icon: "grid",
    zh: "运营概览",
    en: "Overview",
    group: "overview",
  },
  {
    id: "settings",
    icon: "settings",
    zh: "系统设置",
    en: "System settings",
    group: "platform",
  },
  {
    id: "products",
    icon: "spark",
    zh: "模型商品",
    en: "Model products",
    group: "platform",
  },
  {
    id: "tiers",
    icon: "wallet",
    zh: "充值档位",
    en: "Credit packages",
    group: "platform",
  },
  {
    id: "users",
    icon: "group",
    zh: "用户管理",
    en: "Users",
    group: "operations",
  },
  {
    id: "tasks",
    icon: "tasks",
    zh: "生成任务",
    en: "Generation tasks",
    group: "operations",
  },
  {
    id: "orders",
    icon: "wallet",
    zh: "支付订单",
    en: "Payment orders",
    group: "operations",
  },
  {
    id: "adjust",
    icon: "text",
    zh: "积分调账",
    en: "Credit adjustment",
    group: "operations",
  },
  {
    id: "audit",
    icon: "lock",
    zh: "审计日志",
    en: "Audit log",
    group: "operations",
  },
];
export function AdminPage({
  section = "overview",
  query = "",
}: {
  section?: string;
  query?: string;
}) {
  const { system, user, navigate } = useStudio();
  const active =
    adminSections.find((item) => item.id === section) || adminSections[0];
  const target = recordsSignalTarget(active.id, query);
  const userId = recordsUserTarget(active.id, query);
  return (
    <div className="admin-console">
      <header className="admin-heading">
        <div>
          <div className="eyebrow">FRAME / CONTROL ROOM</div>
          <h1>{a(active.zh, active.en)}</h1>
          <p>
            {a(
              "从服务连接到每一笔资金，让创作平台有据可查。",
              "Manage service connections, creation delivery and every credit transaction.",
            )}
          </p>
        </div>
        <div className="admin-identity">
          <span
            className={
              system?.demo || system?.development !== false
                ? "admin-env demo"
                : "admin-env"
            }
          >
            {system?.demo
              ? a("开发演示", "DEMO")
              : system?.development === true
                ? a("开发模式", "DEVELOPMENT")
                : system?.development === false
                  ? a("生产模式", "PRODUCTION")
                  : a("环境未确认", "ENV UNKNOWN")}
          </span>
          <span>{user.display_name || user.email}</span>
        </div>
      </header>
      <nav
        className="admin-mobile-nav"
        aria-label={a("管理后台分区", "Administration sections")}
      >
        {adminSections.map((item) => (
          <button
            key={item.id}
            className={active.id === item.id ? "active" : ""}
            onClick={() => navigate("admin", item.id)}
          >
            {a(item.zh, item.en)}
          </button>
        ))}
      </nav>
      <div className="admin-content" key={active.id}>
        {active.id === "overview" ? (
          <Overview />
        ) : active.id === "settings" ? (
          <SettingsPage />
        ) : active.id === "products" || active.id === "tiers" ? (
          <CatalogPage kind={active.id} />
        ) : active.id === "users" ? (
          <UsersPage query={query} />
        ) : active.id === "adjust" ? (
          <AdjustmentPage initialUserId={userId} />
        ) : (
          <RecordsPage
            key={`${active.id}:${target.recordId}:${target.initialStatus}:${userId}`}
            kind={active.id as "users" | "tasks" | "orders" | "audit"}
            initialRecordId={target.recordId}
            initialStatus={target.initialStatus}
            initialUserId={userId}
          />
        )}
      </div>
    </div>
  );
}
type OverviewData = {
  users: {
    total: number;
    active: number;
    disabled: number;
    unverified: number;
  };
  projects: { active: number; archived: number; deleted: number };
  assets: {
    active_count: number;
    active_bytes: number;
    retained_count: number;
    retained_bytes: number;
  };
  tasks: {
    review_required: number;
    frozen_overdue: number;
    archive_failed: number;
    frozen_count: number;
  };
  wallet: { available_total: number; frozen_total: number };
  payments: { pending_count: number; review_required: number };
  procurement_balance: { status: string; reason: string };
  generated_at: string;
};
function Overview() {
  const { navigate } = useStudio();
  const data = useResource<OverviewData | null>("/admin/overview", null);
  const settings = useResource<{ readiness: Record<string, unknown> } | null>(
    "/admin/settings",
    null,
  );
  const [refreshed, setRefreshed] = useState(0);
  const refresh = () => {
    data.refresh();
    settings.refresh();
    setRefreshed((v) => v + 1);
  };
  if (data.loading || data.error || !data.data)
    return (
      <LoadingError loading={data.loading} error={data.error} retry={refresh} />
    );
  const d = data.data;
  const metrics = [
    [
      a("注册用户", "Registered users"),
      d.users.total.toLocaleString(),
      a(
        `${d.users.active} 正常 · ${d.users.unverified} 待验证`,
        `${d.users.active} active · ${d.users.unverified} unverified`,
      ),
      "users",
    ],
    [
      a("待核查任务", "Tasks requiring review"),
      d.tasks.review_required.toLocaleString(),
      a(
        `${d.tasks.frozen_overdue} 冻结逾期 · ${d.tasks.archive_failed} 交付异常`,
        `${d.tasks.frozen_overdue} overdue · ${d.tasks.archive_failed} delivery failures`,
      ),
      "tasks",
    ],
    [
      a("冻结积分", "Frozen credits"),
      d.wallet.frozen_total.toLocaleString(),
      a(
        `${d.tasks.frozen_count} 个任务等待结算`,
        `${d.tasks.frozen_count} tasks awaiting settlement`,
      ),
      "tasks",
    ],
    [
      a("支付待核查", "Payments requiring review"),
      d.payments.review_required.toLocaleString(),
      a(
        `${d.payments.pending_count} 笔订单等待支付`,
        `${d.payments.pending_count} pending orders`,
      ),
      "orders",
    ],
  ];
  return (
    <>
      <div className="admin-section-toolbar">
        <span className="muted">
          {a("最近统计", "Updated")} {formatDate(d.generated_at)}
        </span>
        <button
          className="button secondary small"
          onClick={refresh}
          disabled={data.loading}
          key={refreshed}
        >
          <Icon name="refresh" size={14} />
          {a("刷新数据", "Refresh")}
        </button>
      </div>
      {data.error && (
        <p role="alert" className="field-error-message">
          {data.error}
        </p>
      )}
      <div className="admin-metrics">
        {metrics.map(([label, value, note, section]) => (
          <button
            key={label}
            className="admin-metric"
            onClick={() => navigate("admin", section)}
          >
            <span>
              {label}
              <Icon name="arrow" size={15} />
            </span>
            <strong>{value}</strong>
            <small>{note}</small>
          </button>
        ))}
      </div>
      <OperationsSignals />
      <div className="admin-overview-columns">
        <section className="admin-panel">
          <div className="admin-panel-title">
            <div>
              <div className="eyebrow">SERVICE READINESS</div>
              <h2>{a("服务配置状态", "Service readiness")}</h2>
            </div>
            <button
              className="text-button"
              onClick={() => navigate("admin", "settings")}
            >
              {a("配置服务", "Configure")} →
            </button>
          </div>
          <LoadingError
            loading={settings.loading}
            error={settings.error}
            retry={settings.refresh}
          />
          {!settings.loading && !settings.error && settings.data && (
            <div className="admin-service-list">
              {Object.entries(settings.data.readiness || {})
                .filter(([name]) => name !== "demo")
                .map(([name, raw]) => {
                  const v =
                    typeof raw === "object" && raw
                      ? (raw as Record<string, unknown>)
                      : { ready: raw };
                  const ready =
                    v.ready === true ||
                    v.configured === true ||
                    v.status === "ready";
                  return (
                    <div key={name}>
                      <span>
                        <i
                          className={`service-indicator ${ready ? "ready" : ""}`}
                        />
                        {{
                          media: a("生成网关", "Generation gateway"),
                          payment: a("易支付", "EasyPay"),
                          smtp: a("邮件服务", "Email delivery"),
                          storage: a("私有存储", "Private storage"),
                          registration: a("公开注册", "Public registration"),
                        }[name] || name}
                      </span>
                      <span className={ready ? "text-green" : "muted"}>
                        {String(
                          v.message ||
                            v.reason ||
                            (ready
                              ? a("已配置", "Configured")
                              : a("待配置", "Needs configuration")),
                        )}
                      </span>
                    </div>
                  );
                })}
            </div>
          )}
          <p className="admin-help">
            {a(
              "连接检测只确认服务可达。真实生成、支付通知与存储交付仍需分别完成发布验收。",
              "Connection checks confirm reachability. Generation, payment callbacks and media delivery require separate release verification.",
            )}
          </p>
        </section>
        <section className="admin-panel">
          <div className="eyebrow">PLATFORM ACTIVITY</div>
          <h2>{a("平台资源", "Platform resources")}</h2>
          <dl className="admin-stat-list">
            <div>
              <dt>{a("活跃项目", "Active projects")}</dt>
              <dd>{d.projects.active.toLocaleString()}</dd>
            </div>
            <div>
              <dt>{a("有效素材", "Active assets")}</dt>
              <dd>{d.assets.active_count.toLocaleString()}</dd>
            </div>
            <div>
              <dt>{a("留存素材容量", "Retained media")}</dt>
              <dd>{(d.assets.retained_bytes / 1073741824).toFixed(2)} GB</dd>
            </div>
            <div>
              <dt>{a("可用积分总额", "Available credits")}</dt>
              <dd>{d.wallet.available_total.toLocaleString()}</dd>
            </div>
          </dl>
          <p className="admin-help">
            {a(
              "容量按数据库逻辑记录统计，包含为历史任务保留的文件。",
              "Storage is measured from logical database records, including retained historical files.",
            )}
          </p>
        </section>
      </div>
      <section className="admin-panel admin-shortcuts">
        <div>
          <div className="eyebrow">OPERATIONS DESK</div>
          <h2>{a("常用管理操作", "Operations desk")}</h2>
        </div>
        {[
          [
            "settings",
            a("连接生成与支付服务", "Connect generation and payments"),
          ],
          [
            "products",
            a("配置模型能力与价格", "Set model capabilities and pricing"),
          ],
          [
            "tasks",
            a("处理交付与冻结异常", "Review delivery and frozen credits"),
          ],
          ["audit", a("追溯管理员操作", "Trace administrator actions")],
        ].map(([id, title]) => (
          <button key={id} onClick={() => navigate("admin", id)}>
            <span>{title}</span>
            <Icon name="arrow" />
          </button>
        ))}
      </section>
      <div className="admin-help">
        {a("采购余额", "Procurement balance")}：
        {d.procurement_balance.reason ||
          a(
            "当前供应端未提供可验证余额接口",
            "The provider has no verified balance endpoint",
          )}
      </div>
    </>
  );
}
