import { useCallback, useEffect, useRef, useState } from "react";
import "./App.css";
import {
  GetSnapshot,
  SaveConnection,
  SetServiceAutoStart,
  SetSilentStart,
  ToggleProxy,
  ToggleService,
  ValidateConnection,
} from "../wailsjs/go/main/App";

type ProxyEntry = {
  name: string;
  type: string;
  localPort: number;
  remotePort: number;
};

type ConnectionSettings = {
  serverAddr: string;
  serverPort: number;
  authToken: string;
  proxies: ProxyEntry[];
};

type StatusView = {
  serviceInstalled: boolean;
  serviceRunning: boolean;
  serviceAutoStart: boolean;
  proxyRunning: boolean;
  headerText: string;
  headerTone: string;
  serviceText: string;
  serviceDetail: string;
  proxyText: string;
  proxyDetail: string;
  silentStartAtLogin: boolean;
  configPath: string;
};

type Snapshot = {
  settings: ConnectionSettings;
  status: StatusView;
  logs: string[];
};

const defaultSettings: ConnectionSettings = {
  serverAddr: "154.12.16.190",
  serverPort: 7000,
  authToken: "sk-962464@zap",
  proxies: [{ name: "default-tcp", type: "tcp", localPort: 3389, remotePort: 13389 }],
};

const emptySnapshot: Snapshot = {
  settings: defaultSettings,
  status: {
    serviceInstalled: false,
    serviceRunning: false,
    serviceAutoStart: false,
    proxyRunning: false,
    headerText: "正在加载",
    headerTone: "primary",
    serviceText: "检测中",
    serviceDetail: "正在读取服务状态。",
    proxyText: "检测中",
    proxyDetail: "正在读取代理状态。",
    silentStartAtLogin: false,
    configPath: "",
  },
  logs: [],
};

/* ── Icons ── */

const IconShield = () => (
  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
  </svg>
);

const IconServer = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <rect x="2" y="2" width="20" height="8" rx="2" ry="2" />
    <rect x="2" y="14" width="20" height="8" rx="2" ry="2" />
    <line x1="6" y1="6" x2="6.01" y2="6" />
    <line x1="6" y1="18" x2="6.01" y2="18" />
  </svg>
);

const IconActivity = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
  </svg>
);

const IconFile = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
    <polyline points="14 2 14 8 20 8" />
    <line x1="16" y1="13" x2="8" y2="13" />
    <line x1="16" y1="17" x2="8" y2="17" />
  </svg>
);

const IconPlus = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
    <line x1="12" y1="5" x2="12" y2="19" />
    <line x1="5" y1="12" x2="19" y2="12" />
  </svg>
);

const IconX = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <line x1="18" y1="6" x2="6" y2="18" />
    <line x1="6" y1="6" x2="18" y2="18" />
  </svg>
);

const IconPlay = () => (
  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
    <polygon points="5 3 19 12 5 21 5 3" />
  </svg>
);

const IconPause = () => (
  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
    <rect x="6" y="4" width="4" height="16" />
    <rect x="14" y="4" width="4" height="16" />
  </svg>
);

const IconCheck = () => (
  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
    <polyline points="22 4 12 14.01 9 11.01" />
  </svg>
);

/* ── Helpers ── */

function serviceDot(s: StatusView): string {
  if (!s.serviceInstalled) return "dot--gray";
  if (s.serviceRunning) return "dot--green";
  return "dot--yellow";
}

function proxyDot(s: StatusView): string {
  if (s.proxyRunning) return "dot--green";
  if (s.serviceRunning) return "dot--yellow";
  return "dot--gray";
}

/** Returns a Set of keys like "3389:tcp" that appear more than once. */
function findDuplicateKeys(proxies: ProxyEntry[]): Set<string> {
  const seen = new Map<string, number>();
  for (const p of proxies) {
    if (p.localPort > 0) {
      const key = `${p.localPort}:${p.type}`;
      seen.set(key, (seen.get(key) ?? 0) + 1);
    }
  }
  const dupes = new Set<string>();
  for (const [key, count] of seen) {
    if (count > 1) dupes.add(key);
  }
  return dupes;
}

/** Returns the set of row indices that have duplicate port+type. */
function duplicateIndices(proxies: ProxyEntry[]): Set<number> {
  const dupes = findDuplicateKeys(proxies);
  const result = new Set<number>();
  proxies.forEach((p, i) => {
    if (p.localPort > 0 && dupes.has(`${p.localPort}:${p.type}`)) {
      result.add(i);
    }
  });
  return result;
}

/** Checks if every proxy row is completely filled and has no duplicate port+type. */
function allProxiesValid(proxies: ProxyEntry[]): boolean {
  if (proxies.length === 0) return false;
  const dupes = findDuplicateKeys(proxies);
  return (
    dupes.size === 0 &&
    proxies.every((p) => p.name.trim() !== "" && p.localPort > 0 && p.remotePort > 0)
  );
}

/* ── Component ── */

function App() {
  const [snapshot, setSnapshot] = useState<Snapshot>(emptySnapshot);
  const [form, setForm] = useState<ConnectionSettings>(defaultSettings);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [saveStatus, setSaveStatus] = useState<"idle" | "saving" | "saved" | "error">("idle");

  const busyRef = useRef<string | null>(null);
  const formDirtyRef = useRef(false);
  const formRef = useRef(form);
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastSavedRef = useRef("");

  // Keep formRef in sync
  useEffect(() => {
    formRef.current = form;
  }, [form]);

  const applySnapshot = (next: Snapshot, syncForm: boolean) => {
    setSnapshot(next);
    if (syncForm) {
      setForm(next.settings);
      lastSavedRef.current = JSON.stringify(next.settings);
      formDirtyRef.current = false;
    }
  };

  const refresh = async () => {
    if (busyRef.current) return;
    try {
      const next = (await GetSnapshot()) as unknown as Snapshot;
      // Re-check after await: user may have changed form while request was in flight
      if (!formDirtyRef.current) {
        applySnapshot(next, true);
      } else {
        setSnapshot(next);
      }
      setError("");
    } catch (err) {
      setError(String(err));
    }
  };

  useEffect(() => {
    void refresh();
    const t = window.setInterval(() => void refresh(), 3000);
    return () => window.clearInterval(t);
  }, []);

  const runAction = async (name: string, action: () => Promise<unknown>) => {
    setBusy(name);
    busyRef.current = name;
    try {
      const next = (await action()) as Snapshot | undefined;
      if (next) applySnapshot(next, true);
      else await refresh();
      setError("");
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(null);
      busyRef.current = null;
    }
  };

  /** Debounced auto-save: waits 800ms after last change. */
  const scheduleAutoSave = useCallback(() => {
    if (saveTimerRef.current) clearTimeout(saveTimerRef.current);
    saveTimerRef.current = setTimeout(async () => {
      const current = formRef.current;
      if (!allProxiesValid(current.proxies)) {
        setSaveStatus("idle");
        return;
      }
      const serialized = JSON.stringify(current);
      if (serialized === lastSavedRef.current) {
        setSaveStatus("idle");
        return;
      }
      setSaveStatus("saving");
      try {
        await SaveConnection(current as any);
        lastSavedRef.current = serialized;
        formDirtyRef.current = false;
        setSaveStatus("saved");
        setTimeout(() => setSaveStatus("idle"), 2000);
      } catch {
        setSaveStatus("error");
        setTimeout(() => setSaveStatus("idle"), 3000);
      }
    }, 800);
  }, []);

  const markDirty = () => {
    formDirtyRef.current = true;
  };

  const updateForm = (patch: Partial<ConnectionSettings>) => {
    setForm((c) => ({ ...c, ...patch }));
    markDirty();
    scheduleAutoSave();
  };

  const updateProxy = (i: number, patch: Partial<ProxyEntry>) => {
    setForm((c) => ({
      ...c,
      proxies: c.proxies.map((p, idx) => (idx === i ? { ...p, ...patch } : p)),
    }));
    markDirty();
    scheduleAutoSave();
  };

  const addProxy = () => {
    const n = form.proxies.length + 1;
    setForm((c) => ({
      ...c,
      proxies: [...c.proxies, { name: `proxy-${n}`, type: "tcp", localPort: 0, remotePort: 0 }],
    }));
    markDirty();
  };

  const removeProxy = (i: number) => {
    setForm((c) => ({ ...c, proxies: c.proxies.filter((_, idx) => idx !== i) }));
    markDirty();
    scheduleAutoSave();
  };

  const s = snapshot.status;
  const dupRows = duplicateIndices(form.proxies);

  const saveStatusText = () => {
    switch (saveStatus) {
      case "saving":
        return "保存中...";
      case "saved":
        return "已保存";
      case "error":
        return "保存失败";
      default:
        return null;
    }
  };

  return (
    <div className="app">
      {/* ── Top bar ── */}
      <header className="topbar">
        <div className="topbar-left">
          <div className="topbar-logo">
            <IconShield />
          </div>
          <span className="topbar-title">FRPC 客户端</span>
          <div className="topbar-divider" />
          <div className="topbar-status">
            <span className={`dot ${serviceDot(s)}`} />
            <span>{s.serviceText}</span>
          </div>
          <div className="topbar-status">
            <span className={`dot ${proxyDot(s)}`} />
            <span>{s.proxyText}</span>
          </div>
        </div>
        <div className="topbar-right">
          {saveStatusText() && (
            <span className={`topbar-save-status topbar-save-status--${saveStatus}`}>
              {saveStatusText()}
            </span>
          )}
          <span className={`topbar-badge ${s.proxyRunning ? "topbar-badge--running" : "topbar-badge--stopped"}`}>
            {s.proxyRunning ? "运行中" : "未连接"}
          </span>
        </div>
      </header>

      {/* ── Sidebar ── */}
      <aside className="sidebar">
        <div className="sidebar-section">
          <span className="sidebar-label">服务</span>
          <div className="sidebar-card">
            <div className="sidebar-stat">
              <div className={`sidebar-stat-icon ${s.serviceRunning ? "sidebar-stat-icon--green" : s.serviceInstalled ? "sidebar-stat-icon--yellow" : "sidebar-stat-icon--gray"}`}>
                <IconServer />
              </div>
              <div className="sidebar-stat-text">
                <span className="sidebar-stat-label">Windows 服务</span>
                <span className="sidebar-stat-value">{s.serviceText}</span>
              </div>
            </div>
          </div>
          <div className="sidebar-card">
            <div className="sidebar-stat">
              <div className={`sidebar-stat-icon ${s.proxyRunning ? "sidebar-stat-icon--green" : "sidebar-stat-icon--gray"}`}>
                <IconActivity />
              </div>
              <div className="sidebar-stat-text">
                <span className="sidebar-stat-label">代理连接</span>
                <span className="sidebar-stat-value">{s.proxyText}</span>
              </div>
            </div>
          </div>
          <div className="sidebar-actions">
            <button
              className="btn btn-primary sidebar-btn"
              disabled={busy !== null}
              onClick={() => runAction("proxy", () => ToggleProxy(form as any) as Promise<unknown>)}
            >
              {s.proxyRunning ? <IconPause /> : <IconPlay />}
              {busy === "proxy" ? "处理中..." : s.proxyRunning ? "关闭代理" : "启动代理"}
            </button>
            <button
              className="btn btn-secondary sidebar-btn"
              disabled={busy !== null}
              onClick={() => runAction("service", () => ToggleService() as Promise<unknown>)}
            >
              <IconServer />
              {busy === "service" ? "处理中..." : s.serviceInstalled ? "卸载服务" : "安装服务"}
            </button>
            <button
              className="btn btn-secondary sidebar-btn"
              disabled={busy !== null}
              onClick={() => runAction("validate", () => ValidateConnection(form as any) as Promise<unknown>)}
            >
              <IconCheck />
              {busy === "validate" ? "校验中..." : "校验配置"}
            </button>
          </div>
        </div>

        <div className="sidebar-section">
          <span className="sidebar-label">选项</span>
          <label className="sidebar-toggle">
            <span className="sidebar-toggle-label">随系统启动</span>
            <input
              type="checkbox"
              checked={s.serviceAutoStart}
              onChange={(e) =>
                void runAction("autostart", () =>
                  SetServiceAutoStart(e.target.checked) as Promise<unknown>,
                )
              }
            />
          </label>
          <label className="sidebar-toggle">
            <span className="sidebar-toggle-label">静默启动</span>
            <input
              type="checkbox"
              checked={s.silentStartAtLogin}
              onChange={(e) =>
                void runAction("silentstart", () =>
                  SetSilentStart(e.target.checked) as Promise<unknown>,
                )
              }
            />
          </label>
        </div>

        <div className="sidebar-section">
          <span className="sidebar-label">配置文件</span>
          <div className="sidebar-config-path">
            {s.configPath || "C:\\ProgramData\\frp\\frpc.toml"}
          </div>
        </div>
      </aside>

      {/* ── Main ── */}
      <main className="main">
        {/* Connection */}
        <section className="card">
          <div className="card-header">
            <div className="card-header-left">
              <div className="card-icon card-icon--blue"><IconServer /></div>
              <div>
                <div className="card-title">服务器连接</div>
                <div className="card-desc">配置 FRP 服务器的地址和认证信息</div>
              </div>
            </div>
          </div>
          <div className="card-body">
            <div className="form-row">
              <label className="form-field">
                <span className="form-label">服务端地址</span>
                <input
                  className="form-input"
                  value={form.serverAddr}
                  onChange={(e) => updateForm({ serverAddr: e.target.value })}
                />
              </label>
              <label className="form-field form-field--narrow">
                <span className="form-label">端口</span>
                <input
                  className="form-input"
                  type="number"
                  value={form.serverPort}
                  onChange={(e) => updateForm({ serverPort: Number(e.target.value) || 7000 })}
                />
              </label>
              <label className="form-field form-field--grow">
                <span className="form-label">访问密钥</span>
                <input
                  className="form-input"
                  type="password"
                  value={form.authToken}
                  onChange={(e) => updateForm({ authToken: e.target.value })}
                />
              </label>
            </div>
          </div>
        </section>

        {/* Proxy list */}
        <section className="card">
          <div className="card-header">
            <div className="card-header-left">
              <div className="card-icon card-icon--green"><IconActivity /></div>
              <div>
                <div className="card-title">代理规则</div>
                <div className="card-desc">管理多端口转发，支持 TCP 和 UDP。同一端口可同时代理 TCP 和 UDP，但不能重复相同协议。</div>
              </div>
            </div>
          </div>
          <div className="card-body" style={{ padding: 0 }}>
            <div className="proxy-table-wrap">
              <table className="proxy-table">
                <thead>
                  <tr>
                    <th className="col-idx">#</th>
                    <th className="col-name">名称</th>
                    <th className="col-type">类型</th>
                    <th className="col-port">本地端口</th>
                    <th className="col-port">远程端口</th>
                    <th className="col-action"></th>
                  </tr>
                </thead>
                <tbody>
                  {form.proxies.map((proxy, idx) => {
                    const isDup = dupRows.has(idx);
                    return (
                      <tr key={idx} className={isDup ? "proxy-row--dup" : undefined}>
                        <td className="col-idx">
                          <span className="proxy-idx">{idx + 1}</span>
                        </td>
                        <td className="col-name">
                          <input
                            className={`proxy-cell-input ${isDup ? "proxy-cell-input--error" : ""}`}
                            value={proxy.name}
                            onChange={(e) => updateProxy(idx, { name: e.target.value })}
                            placeholder="proxy-name"
                          />
                        </td>
                        <td className="col-type">
                          <select
                            className={`proxy-cell-select ${isDup ? "proxy-cell-input--error" : ""}`}
                            value={proxy.type}
                            onChange={(e) => updateProxy(idx, { type: e.target.value })}
                          >
                            <option value="tcp">TCP</option>
                            <option value="udp">UDP</option>
                          </select>
                        </td>
                        <td className="col-port">
                          <input
                            className={`proxy-cell-input ${isDup ? "proxy-cell-input--error" : ""}`}
                            type="number"
                            value={proxy.localPort || ""}
                            onChange={(e) => updateProxy(idx, { localPort: Number(e.target.value) || 0 })}
                            placeholder="0"
                          />
                        </td>
                        <td className="col-port">
                          <input
                            className="proxy-cell-input"
                            type="number"
                            value={proxy.remotePort || ""}
                            onChange={(e) => updateProxy(idx, { remotePort: Number(e.target.value) || 0 })}
                            placeholder="0"
                          />
                        </td>
                        <td className="col-action">
                          <button
                            className="proxy-del-btn"
                            onClick={() => removeProxy(idx)}
                            disabled={form.proxies.length <= 1}
                            title="删除"
                          >
                            <IconX />
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            {dupRows.size > 0 && (
              <div className="proxy-dup-hint">
                存在相同端口和类型的重复代理，请修改后再保存。
              </div>
            )}
            <div className="proxy-add-row">
              <button className="proxy-add-btn" onClick={addProxy}>
                <IconPlus /> 添加代理
              </button>
            </div>
          </div>
        </section>

        {/* Logs */}
        <section className="card">
          <div className="card-header">
            <div className="card-header-left">
              <div className="card-icon card-icon--purple"><IconFile /></div>
              <div>
                <div className="card-title">运行日志</div>
              </div>
            </div>
          </div>
          <div className="card-body">
            <div className="log-box">
              {snapshot.logs.length === 0 ? (
                <p className="log-empty">还没有日志，尝试启动代理。</p>
              ) : (
                snapshot.logs
                  .slice()
                  .reverse()
                  .map((line, i) => (
                    <div key={`${line}-${i}`} className="log-line">
                      {line}
                    </div>
                  ))
              )}
            </div>
          </div>
        </section>
      </main>

      {error ? <div className="error-banner">{error}</div> : null}
    </div>
  );
}

export default App;
