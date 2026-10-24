import { t, useLocale } from "./i18n";
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  api,
  ApiError,
  errorMessage,
  isCurrentSession,
  sessionVersion,
  setCSRF,
  waitForSessionChanges,
} from "./api";
import { Auth } from "./Auth";
import { PoliciesModal } from "./PoliciesModal";
import type { Identity, System, User, Wallet } from "./types";
import {
  Icon,
  LoadingError,
  LocaleSwitcher,
  Logo,
  Notice,
  useResource,
} from "./ui";
import { Projects } from "./Projects";
import { ProductSwap } from "./ProductSwap";
import { Workspace } from "./Workspace";
import { routeFromLocation, type Route } from "./routing";
import {
  createNavigationGuard,
  type NavigationGuard,
} from "./navigation-guard";
import { AssetsPage, TasksPage, WalletPage, AccountPage } from "./pages";
import { AdminPage, adminSections } from "./admin/AdminPage";
import { a } from "./admin/common";
const currentRoute = (): Route => routeFromLocation(window.location);
const StudioContext = createContext<{
  user: User;
  system: System | null;
  notify: (message: string, error?: boolean) => void;
  navigate: (page: string, id?: string) => void;
  refreshWallet: () => void;
  refreshSystem: () => void;
  logout: () => Promise<void>;
  endSession: (version: number, message?: string) => void;
} | null>(null);
export const useStudio = () => useContext(StudioContext)!;
export function App() {
  const locale = useLocale();
  const [identity, setIdentity] = useState<Identity | null>(null),
    [boot, setBoot] = useState(true),
    [bootError, setBootError] = useState(""),
    [sessionMessage, setSessionMessage] = useState(""),
    [route, setRoute] = useState<Route>(currentRoute),
    [toast, setToast] = useState<{
      message: string;
      error?: boolean;
    } | null>(null),
    [walletVersion, setWalletVersion] = useState(0),
    [mobileNav, setMobileNav] = useState(false),
    [policiesOpen, setPoliciesOpen] = useState(false),
    [policiesRefreshing, setPoliciesRefreshing] = useState(false);
  const navigation = useRef<NavigationGuard | null>(null);
  const live = useRef(false);
  const sessionRequest = useRef(0);
  const logoutRequest = useRef<number | null>(null);
  const identityVersion = sessionVersion();
  const system = useResource<System | null>("/system", null);
  useEffect(() => {
    if (!system.loading) setPoliciesRefreshing(false);
  }, [system.loading]);
  useEffect(() => {
    document.documentElement.lang = locale;
    document.title = `${system.data?.brand || "FRAME"} / ${locale === "en" ? "Creative Studio" : "创作工作室"}`;
  }, [locale, system.data?.brand]);
  async function loadSession() {
    const version = sessionVersion();
    const request = ++sessionRequest.current;
    const current = () =>
      live.current &&
      request === sessionRequest.current &&
      isCurrentSession(version);
    setBoot(true);
    setBootError("");
    setSessionMessage("");
    try {
      await waitForSessionChanges();
      if (!current()) return;
      const me = await api<Identity>("/me");
      if (!current()) return;
      setCSRF(me.csrf_token);
      setIdentity(me);
      setBoot(false);
    } catch (error) {
      if (!current()) return;
      if (!(error instanceof ApiError && error.status === 401))
        setBootError(errorMessage(error));
      setBoot(false);
    }
  }
  function endSession(version: number, message = "", clearDrafts = false) {
    if (!live.current || !isCurrentSession(version)) return;
    if (clearDrafts && identity) {
      try {
        for (const key of Object.keys(localStorage))
          if (key.startsWith(`frame:draft:${identity.user.id}:`))
            localStorage.removeItem(key);
      } catch {
        // Unavailable browser storage must not prevent ending the session.
      }
    }
    setCSRF("");
    setIdentity(null);
    setToast(null);
    setMobileNav(false);
    setBoot(false);
    setBootError("");
    setSessionMessage(message);
  }
  useEffect(() => {
    live.current = true;
    const guard = createNavigationGuard(
      window,
      () =>
        window.dispatchEvent(
          new CustomEvent("frame:before-navigate", { cancelable: true }),
        ),
      () => {
        setRoute(currentRoute());
        setMobileNav(false);
      },
    );
    navigation.current = guard;
    void loadSession();
    const unauthorized = () => {
      endSession(sessionVersion());
      setBootError("登录已过期，请重新登录；本地草稿会继续保留。");
    };
    window.addEventListener("frame:unauthorized", unauthorized);
    return () => {
      live.current = false;
      sessionRequest.current += 1;
      guard.dispose();
      if (navigation.current === guard) navigation.current = null;
      window.removeEventListener("frame:unauthorized", unauthorized);
    };
  }, []);
  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 5000);
    return () => clearTimeout(timer);
  }, [toast]);
  const navigate = (page: string, id?: string) => {
    navigation.current?.navigate(`/${page}${id ? `/${id}` : ""}`);
  };
  const notify = (message: string, error?: boolean) =>
    setToast({ message, error });
  const logout = async () => {
    if (
      !live.current ||
      !isCurrentSession(identityVersion) ||
      logoutRequest.current === identityVersion
    )
      return;
    logoutRequest.current = identityVersion;
    try {
      await api("/auth/logout", "POST", {});
    } catch (error) {
      if (!live.current || !isCurrentSession(identityVersion)) return;
      if (!(error instanceof ApiError && error.status === 401)) throw error;
    } finally {
      if (logoutRequest.current === identityVersion)
        logoutRequest.current = null;
    }
    if (!live.current || !isCurrentSession(identityVersion)) return;
    endSession(identityVersion, "", true);
    navigate("projects");
  };
  const refreshPolicies = () => {
    setPoliciesRefreshing(true);
    system.refresh();
  };
  const openPolicies = () => {
    refreshPolicies();
    setPoliciesOpen(true);
  };
  const policies = policiesOpen && (
    <PoliciesModal
      system={system.data}
      loading={system.loading || policiesRefreshing}
      error={system.error}
      retry={refreshPolicies}
      onClose={() => setPoliciesOpen(false)}
    />
  );
  if (boot)
    return (
      <div className="boot">
        <Logo />
        <LoadingError loading error="" />
      </div>
    );
  if (!identity)
    return (
      <>
        <Auth
          system={system.data}
          sessionNotice={
            <>
              {sessionMessage && <Notice>{t(sessionMessage)}</Notice>}
              {bootError && (
                <Notice error>
                  {t(bootError)}{" "}
                  <button
                    className="text-button"
                    onClick={() => void loadSession()}
                  >
                    {t("重试连接")}
                  </button>
                </Notice>
              )}
            </>
          }
          onPolicies={openPolicies}
          onLogin={(value) => {
            setCSRF(value.csrf_token);
            setIdentity(value);
            setBootError("");
            setSessionMessage("");
            setToast(null);
            setWalletVersion((v) => v + 1);
          }}
        />
        {policies}
      </>
    );
  const creatorNav = [
    {
      page: "projects",
      icon: "grid",
      label: t("项目空间"),
      en: "PROJECTS",
    },
    {
      page: "product-swap",
      icon: "refresh",
      label: t("商品替换"),
      en: "PRODUCT SWAP",
    },
    {
      page: "assets",
      icon: "image",
      label: t("素材库"),
      en: "ASSETS",
    },
    {
      page: "tasks",
      icon: "tasks",
      label: t("任务中心"),
      en: "TASKS",
    },
  ];
  const nav =
    route.page === "admin"
      ? adminSections.map((item) => ({
          page: "admin",
          id: item.id,
          icon: item.icon,
          label: a(item.zh, item.en),
          en: "",
        }))
      : creatorNav.map((item) => ({ ...item, id: undefined }));
  const pageNames: Record<string, string> = {
    projects: t("项目空间"),
    project: t("创作工作台"),
    assets: t("素材库"),
    "product-swap": t("商品替换"),
    tasks: t("任务中心"),
    wallet: t("积分钱包"),
    account: t("账户设置"),
    admin: t("运营后台"),
  };
  const context = {
    user: identity.user,
    system: system.data,
    notify,
    navigate,
    refreshWallet: () => setWalletVersion((v) => v + 1),
    refreshSystem: system.refresh,
    logout,
    endSession,
  };
  return (
    <StudioContext.Provider key={identityVersion} value={context}>
      <div
        className={`app ${route.page === "project" ? "app-workspace" : ""} ${route.page === "admin" ? "app-admin" : ""}`}
      >
        <aside className={`sidebar ${mobileNav ? "sidebar-open" : ""}`}>
          <a
            href="#/projects"
            className="brand-link"
            onClick={(event) => {
              if (
                event.button !== 0 ||
                event.metaKey ||
                event.ctrlKey ||
                event.shiftKey ||
                event.altKey
              )
                return;
              event.preventDefault();
              navigate("projects");
            }}
          >
            <Logo />
          </a>
          <div className="workspace-label">
            <span className="workspace-avatar">
              {identity.user.display_name?.slice(0, 1) || "F"}
            </span>
            <span>
              {identity.user.display_name || t("我的工作室")}
              <small>
                {route.page === "admin"
                  ? a("平台管理员", "Platform administrator")
                  : t("个人创作空间")}
              </small>
            </span>
            <span className="live-dot" />
          </div>
          <div className="nav-caption">
            {route.page === "admin" ? "CONTROL ROOM" : "WORKSPACE"}
          </div>
          <nav>
            {nav.map((item) => (
              <button
                key={item.id || item.page}
                className={`nav-item ${(item.page === "admin" ? (route.id || "overview") === item.id : route.page === item.page || (item.page === "projects" && route.page === "project")) ? "active" : ""}`}
                onClick={() => navigate(item.page, item.id)}
              >
                <Icon name={item.icon} />
                <span>{item.label}</span>
                <small>{item.en}</small>
              </button>
            ))}
          </nav>
          <div className="sidebar-lower">
            {route.page !== "admin" && (
              <button
                className={`nav-item ${route.page === "wallet" ? "active" : ""}`}
                onClick={() => navigate("wallet")}
              >
                <Icon name="wallet" />
                <span>{t("积分钱包")}</span>
              </button>
            )}
            {identity.user.role === "admin" && route.page !== "admin" && (
              <button
                className={`nav-item ${route.page === "admin" ? "active" : ""}`}
                onClick={() => navigate("admin")}
              >
                <Icon name="settings" />
                <span>{t("运营后台")}</span>
              </button>
            )}
            {route.page === "admin" && (
              <button className="nav-item" onClick={() => navigate("projects")}>
                <Icon name="back" />
                <span>{a("返回创作工作室", "Back to studio")}</span>
              </button>
            )}
            {route.page !== "admin" && (
              <WalletCard
                version={walletVersion}
                onRecharge={() => navigate("wallet")}
              />
            )}
            <button className="nav-item" onClick={openPolicies}>
              <Icon name="text" />
              <span>{t("服务政策与支持")}</span>
            </button>
            <button className="user-card" onClick={() => navigate("account")}>
              <span className="user-avatar">
                {identity.user.display_name?.slice(0, 1) ||
                  identity.user.email.slice(0, 1)}
              </span>
              <span>
                <strong>{identity.user.display_name || t("创作者")}</strong>
                <small>{identity.user.email}</small>
              </span>
              <Icon name="settings" size={16} />
            </button>
          </div>
        </aside>
        <div className="main-shell">
          <header className="topbar">
            <button
              className="icon-button mobile-nav-button"
              onClick={() => setMobileNav(!mobileNav)}
              aria-label={t("打开导航")}
            >
              <Icon name="grid" />
            </button>
            <div className="breadcrumb">
              {route.page === "admin"
                ? a("平台管理", "Administration")
                : t("我的工作室")}
              <span>/</span>{" "}
              <strong>
                {route.page === "admin"
                  ? a(
                      (
                        adminSections.find(
                          (item) => item.id === (route.id || "overview"),
                        ) || adminSections[0]
                      ).zh,
                      (
                        adminSections.find(
                          (item) => item.id === (route.id || "overview"),
                        ) || adminSections[0]
                      ).en,
                    )
                  : pageNames[route.page] || t("项目空间")}
              </strong>
            </div>
            <div className="topbar-right">
              <LocaleSwitcher />
              {system.data?.demo && (
                <span className="demo-badge">{t("开发演示环境")}</span>
              )}
              <span className="studio-status">
                <i />
                {t("云端工作室")}
              </span>
              <button
                className="icon-button"
                onClick={() => navigate("account")}
                aria-label={t("账户设置")}
              >
                <Icon name="settings" size={17} />
              </button>
            </div>
          </header>
          <main
            className={
              route.page === "project" ? "workspace-main" : "page-main"
            }
          >
            {route.page === "project" && route.id ? (
              <Workspace
                key={route.id}
                projectId={route.id}
                query={route.query}
              />
            ) : route.page === "product-swap" ? (
              <ProductSwap />
            ) : route.page === "assets" ? (
              <AssetsPage />
            ) : route.page === "tasks" ? (
              <TasksPage initialTaskId={route.id} />
            ) : route.page === "wallet" ? (
              <WalletPage />
            ) : route.page === "account" ? (
              <AccountPage />
            ) : route.page === "admin" && identity.user.role === "admin" ? (
              <AdminPage section={route.id} query={route.query} />
            ) : (
              <Projects />
            )}
          </main>
        </div>
        {toast && (
          <div
            className={`toast ${toast.error ? "toast-error" : ""}`}
            role="status"
          >
            <Icon name={toast.error ? "info" : "check"} />
            {toast.message}
            <button
              className="icon-button"
              onClick={() => setToast(null)}
              aria-label={t("关闭通知")}
            >
              <Icon name="close" size={14} />
            </button>
          </div>
        )}
      </div>
      {policies}
    </StudioContext.Provider>
  );
}
function WalletCard({
  version,
  onRecharge,
}: {
  version: number;
  onRecharge: () => void;
}) {
  const wallet = useResource<Wallet | null>("/wallet", null, version);
  return (
    <div className="credit-card">
      <span>
        {t("可用积分")}
        <Icon name="spark" size={14} />
      </span>
      <strong>
        {wallet.data ? wallet.data.available.toLocaleString() : "—"}
        <small>CREDITS</small>
      </strong>
      {wallet.data && wallet.data.frozen > 0 && (
        <small>
          {t("冻结中")}
          {wallet.data.frozen.toLocaleString()}
        </small>
      )}
      <button onClick={onRecharge}>
        {t("充值积分")}
        <Icon name="arrow" size={14} />
      </button>
    </div>
  );
}
export function PageHeading({
  eyebrow,
  title,
  description,
  children,
}: {
  eyebrow: string;
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <div className="page-heading">
      <div>
        <div className="eyebrow">{eyebrow}</div>
        <h1>
          {title}
          <span className="title-dot">.</span>
        </h1>
        <p>{description}</p>
      </div>
      <div className="heading-actions">{children}</div>
    </div>
  );
}
