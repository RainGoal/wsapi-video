import { t } from "./i18n";
import { policyFields, policyText } from "./policies";
import type { System } from "./types";
import { LoadingError, Modal } from "./ui";
import "./policies.css";

export function PoliciesModal({
  system,
  loading,
  error,
  retry,
  onClose,
}: {
  system: System | null;
  loading: boolean;
  error: string;
  retry: () => void;
  onClose: () => void;
}) {
  return (
    <Modal title={t("服务政策与支持")} onClose={onClose} wide>
      <LoadingError loading={loading} error={error} retry={retry} />
      {!loading && !error && (
        <div className="service-policies">
          {policyFields.map(({ key, title }) => {
            const text = policyText(system?.[key]);
            return (
              <section key={key}>
                <h3>{t(title)}</h3>
                <p className={text ? "policy-copy" : "policy-copy muted"}>
                  {text || t("待运营公布")}
                </p>
              </section>
            );
          })}
          <section>
            <h3>{t("支持邮箱")}</h3>
            <p className="policy-copy">
              {policyText(system?.support_email) || t("待运营公布")}
            </p>
          </section>
        </div>
      )}
    </Modal>
  );
}
