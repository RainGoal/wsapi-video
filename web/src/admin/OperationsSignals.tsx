import { useStudio } from "../App";
import { getLocale } from "../i18n";
import { Icon, LoadingError, Notice, useResource } from "../ui";
import { a } from "./common";
import {
  signalDefinition,
  signalDestination,
  type OperationsSnapshot,
} from "./operation-signals";
import "./operations-signals.css";

function signalTime(value: string | null) {
  if (!value || !Number.isFinite(Date.parse(value))) return "—";
  return new Intl.DateTimeFormat(getLocale(), {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

export function OperationsSignals() {
  const { navigate } = useStudio();
  const resource = useResource<OperationsSnapshot | null>(
    "/admin/operations/signals",
    null,
  );
  const snapshot = resource.data;
  function open(code: string, sampleId?: string) {
    const destination = signalDestination(code, sampleId);
    if (destination) navigate("admin", destination);
  }
  return (
    <section className="admin-panel operations-signals">
      <div className="admin-panel-title">
        <div>
          <div className="eyebrow">OPERATIONS / READ-ONLY SNAPSHOT</div>
          <h2>{a("待处理信号", "Operational signals")}</h2>
        </div>
        <button
          className="button secondary small"
          disabled={resource.loading}
          onClick={resource.refresh}
        >
          <Icon name="refresh" size={14} />
          {a("刷新信号", "Refresh signals")}
        </button>
      </div>
      <p className="admin-help">
        {a(
          "这里只提供本地只读快照，实际外部通知待配置。不同类别可能包含同一任务；邮件重试是待投递邮件的子集，不能加总为唯一异常数量。",
          "This is a read-only local snapshot. External notifications still require configuration. Tasks can appear in multiple categories; mail retries are included in pending mail. Counts must not be added as unique incidents.",
        )}
      </p>
      <LoadingError
        loading={resource.loading}
        error={resource.error}
        retry={resource.refresh}
      />
      {!resource.loading && !resource.error && !snapshot && (
        <Notice>
          {a(
            "未收到信号快照，请刷新。当前状态不能用于判断服务健康。",
            "No signal snapshot was returned. Refresh; this state cannot establish service health.",
          )}
        </Notice>
      )}
      {!resource.loading && !resource.error && snapshot && (
        <>
          <div className="signal-snapshot-meta">
            <span>
              {a("快照时间", "Snapshot time")}{" "}
              <time
                dateTime={snapshot.generated_at}
                title={snapshot.generated_at}
              >
                {signalTime(snapshot.generated_at)}
              </time>
            </span>
            <span>
              {a("人工核查阈值", "Review threshold")}{" "}
              {snapshot.review_after_seconds.toLocaleString()}{" "}
              {a("秒", "seconds")}
            </span>
          </div>
          <p className="admin-help">
            {a(
              `每类仅显示最早 ${Math.min(snapshot.sample_limit, 5)} 条定位样本，计数涵盖该类别全部记录。时间为记录创建时间；租约类别为租约到期时间，并非首次异常时间。`,
              `Each category shows only its earliest ${Math.min(snapshot.sample_limit, 5)} samples; its count covers all matching records. Times are record creation times, except expired leases use lease expiry; they are not the first incident time.`,
            )}
          </p>
          {snapshot.signals.length ? (
            <div className="signal-grid">
              {snapshot.signals.map((signal) => {
                const definition = signalDefinition(signal.code);
                const mail = definition?.section === "settings";
                const samples = (signal.samples || []).slice(0, 5);
                return (
                  <article className="signal-card" key={signal.code}>
                    <div className="signal-card-heading">
                      <h3>
                        {definition ? a(...definition.title) : signal.code}
                      </h3>
                      <strong
                        className={
                          signal.count > 0
                            ? "signal-count"
                            : "signal-count muted"
                        }
                      >
                        {signal.count.toLocaleString()}
                      </strong>
                    </div>
                    {definition && (
                      <p className="signal-description">
                        {a(...definition.description)}
                      </p>
                    )}
                    <div className="signal-oldest">
                      <span>
                        {signal.code === "task_lease_expired"
                          ? a("最早租约到期", "Earliest lease expiry")
                          : a("最早记录创建", "Earliest record created")}
                      </span>
                      <time
                        dateTime={signal.oldest_at || undefined}
                        title={signal.oldest_at || undefined}
                      >
                        {signalTime(signal.oldest_at)}
                      </time>
                    </div>
                    {samples.length ? (
                      <ul className="signal-samples">
                        {samples.map((sample) => (
                          <li key={sample.id}>
                            {mail || !definition ? (
                              <code>{sample.id}</code>
                            ) : (
                              <button
                                className="signal-object"
                                onClick={() => open(signal.code, sample.id)}
                                aria-label={a(
                                  `查看本地记录 ${sample.id}`,
                                  `Inspect local record ${sample.id}`,
                                )}
                              >
                                <code>{sample.id}</code>
                                <Icon name="arrow" size={13} />
                              </button>
                            )}
                            <div>
                              <time
                                dateTime={sample.occurred_at}
                                title={sample.occurred_at}
                              >
                                {signalTime(sample.occurred_at)}
                              </time>
                              <span>
                                {a("尝试次数", "Attempts")} {sample.attempts}
                              </span>
                            </div>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="signal-empty">
                        {signal.count === 0
                          ? a(
                              "当前快照中此类别无匹配记录。",
                              "No matching records in this category at snapshot time.",
                            )
                          : a(
                              "未返回定位样本，请进入列表排查。",
                              "No samples returned. Inspect the records list.",
                            )}
                      </p>
                    )}
                    {mail && (
                      <p className="signal-description">
                        {a(
                          "进入系统设置 → 邮件服务，检查已保存的 SMTP 配置与连接状态。",
                          "Open System settings → Email delivery to inspect the saved SMTP configuration and connection status.",
                        )}
                      </p>
                    )}
                    {definition && (
                      <button
                        className="text-button signal-destination"
                        onClick={() => open(signal.code)}
                      >
                        {mail
                          ? a("排查邮件设置", "Inspect email settings")
                          : a("进入对应列表排查", "Inspect related records")}
                        <Icon name="arrow" size={13} />
                      </button>
                    )}
                  </article>
                );
              })}
            </div>
          ) : (
            <Notice>
              {a(
                "本次快照未返回信号分类，不能据此判断服务健康。请刷新或检查接口。",
                "This snapshot returned no signal categories. It cannot establish service health. Refresh or inspect the endpoint.",
              )}
            </Notice>
          )}
        </>
      )}
    </section>
  );
}
