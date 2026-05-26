import { QueryClient, QueryClientProvider, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { Activity, Check, Cloud, Copy, LogOut, MonitorDot, Play, Plus, RefreshCw, Server, ShieldCheck, Terminal, Trash2, XCircle } from "lucide-react";
import { FormEvent, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

type AuthUser = {
  id: string;
  email: string;
};

type ProviderInfo = {
  name: string;
  label: string;
  capabilities: string[];
};

type Machine = {
  name: string;
  provider?: string;
  provider_label?: string;
  provider_id?: string;
  public_ipv4?: string;
  region?: string;
  size?: string;
  image?: string;
  ssh_user?: string;
  preview_hostname?: string;
  preview_url?: string;
  source_path?: string;
  project_path?: string;
  repo_url?: string;
  branch?: string;
  last_synced_at?: string;
  bootstrap_complete?: boolean;
  created_at?: string;
  updated_at?: string;
};

type LoginResponse = {
  token: string;
  user?: AuthUser;
};

type MachineResponse = {
  machine: Machine;
  status?: string;
};

type MachinesResponse = {
  machines: Machine[];
};

type ProvidersResponse = {
  providers: ProviderInfo[];
};

type DeviceStatusResponse = {
  user_code: string;
  status: "pending" | "approved" | "denied";
};

type ConnectResponse = MachineResponse & {
  connect: {
    ssh: string;
    cli: string;
    cli_run: string;
  };
};

const configuredAPIURL = (import.meta.env.VITE_YOLOBOX_API_URL || "").replace(/\/+$/, "");
const apiBaseURL = configuredAPIURL || (window.location.hostname === "app.yolobox.dev" ? "https://api.yolobox.dev" : "");
const tokenKey = "yolobox.backend.token";
const queryClient = new QueryClient();

const rootRoute = createRootRoute({
  component: AppShell,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: ConsoleRoute,
});

const deviceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/device",
  component: ConsoleRoute,
});

const router = createRouter({
  routeTree: rootRoute.addChildren([indexRoute, deviceRoute]),
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

function AppShell() {
  return <ConsoleRoute />;
}

function ConsoleRoute() {
  const [token, setToken] = useState(() => localStorage.getItem(tokenKey) || "");
  const [deviceUserCode, setDeviceUserCode] = useState(() => readDeviceUserCode());
  const session = useQuery({
    queryKey: ["session", token],
    enabled: token.length > 0,
    retry: false,
    queryFn: () => apiFetch<{ authenticated: boolean; provider: string; user: AuthUser }>("/v1/auth/whoami", token),
  });
  const authenticated = Boolean(token && session.data?.authenticated);

  function handleToken(nextToken: string) {
    localStorage.setItem(tokenKey, nextToken);
    setToken(nextToken);
  }

  function handleLogout() {
    if (token) {
      void apiFetch("/v1/auth/sign-out", token, { method: "POST", body: {} }).catch(() => undefined);
    }
    localStorage.removeItem(tokenKey);
    setToken("");
    queryClient.clear();
  }

  function clearDevicePrompt() {
    setDeviceUserCode("");
    if (window.location.pathname === "/device") {
      window.history.replaceState(null, "", "/");
    }
  }

  return (
    <main className="console">
      <div className="backdrop-grid" />
      <header className="topbar">
        <div className="brand">
          <div className="brand-mark"><Terminal size={18} /></div>
          <div>
            <strong>yolobox</strong>
            <span>remote console</span>
          </div>
        </div>
        <div className="topbar-actions">
          <span className="pulse"><Activity size={14} /> api</span>
          {authenticated ? (
            <button className="icon-button" type="button" onClick={handleLogout} title="Log out">
              <LogOut size={17} />
            </button>
          ) : null}
        </div>
      </header>

      {authenticated ? (
        deviceUserCode ? (
          <DeviceGrantPanel token={token} user={session.data?.user} userCode={deviceUserCode} onDone={clearDevicePrompt} />
        ) : (
          <Dashboard token={token} user={session.data?.user} />
        )
      ) : (
        <AccessPanel onToken={handleToken} deviceUserCode={deviceUserCode} />
      )}
    </main>
  );
}

function AccessPanel({ onToken, deviceUserCode }: { onToken: (token: string) => void; deviceUserCode?: string }) {
  const [mode, setMode] = useState<"signin" | "signup">("signup");
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const mutation = useMutation({
    mutationFn: async () => {
      const endpoint = mode === "signup" ? "/v1/auth/sign-up/email" : "/v1/auth/sign-in/email";
      return apiFetch<LoginResponse>(endpoint, "", {
        method: "POST",
        body: {
          email,
          password,
          ...(mode === "signup" ? { name: name || email.split("@")[0] } : {}),
        },
      });
    },
    onSuccess: (data) => onToken(data.token),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    mutation.mutate();
  }

  return (
    <section className="access-layout">
      <div className="terminal-panel">
        <div className="terminal-title">
          <span />
          <span />
          <span />
        </div>
        <pre>{`$ yolobox remote list
provider     machine        status
backend      source         truth
cli          zero-state     client
ui           account        control`}</pre>
      </div>
      <form className="auth-panel" onSubmit={submit}>
        <div className="segmented">
          <button type="button" className={mode === "signup" ? "active" : ""} onClick={() => setMode("signup")}>Sign up</button>
          <button type="button" className={mode === "signin" ? "active" : ""} onClick={() => setMode("signin")}>Sign in</button>
        </div>
        <label>
          Email
          <input value={email} onChange={(event) => setEmail(event.target.value)} type="email" autoComplete="email" required />
        </label>
        {mode === "signup" ? (
          <label>
            Name
            <input value={name} onChange={(event) => setName(event.target.value)} autoComplete="name" />
          </label>
        ) : null}
        <label>
          Password
          <input value={password} onChange={(event) => setPassword(event.target.value)} type="password" autoComplete={mode === "signup" ? "new-password" : "current-password"} required />
        </label>
        {deviceUserCode ? <p className="hint">Sign in here to approve CLI access for code <code>{formatUserCode(deviceUserCode)}</code>.</p> : null}
        <button className="primary-button" type="submit" disabled={mutation.isPending}>
          <Play size={16} />
          {mutation.isPending ? "Working" : mode === "signup" ? "Create account" : "Open console"}
        </button>
        {mutation.error ? <p className="error">{(mutation.error as Error).message}</p> : null}
      </form>
    </section>
  );
}

function DeviceGrantPanel({ token, user, userCode, onDone }: {
  token: string;
  user?: AuthUser;
  userCode: string;
  onDone: () => void;
}) {
  const verify = useQuery({
    queryKey: ["device-login", userCode, token],
    retry: false,
    queryFn: () => apiFetch<DeviceStatusResponse>(`/v1/auth/device?user_code=${encodeURIComponent(userCode)}`, token),
  });
  const approve = useMutation({
    mutationFn: () => apiFetch<{ success: boolean }>("/v1/auth/device/approve", token, { method: "POST", body: { userCode } }),
  });
  const deny = useMutation({
    mutationFn: () => apiFetch<{ success: boolean }>("/v1/auth/device/deny", token, { method: "POST", body: { userCode } }),
  });
  const finished = approve.isSuccess || deny.isSuccess;
  const blocked = verify.isLoading || verify.isError || finished || approve.isPending || deny.isPending;

  return (
    <section className="access-layout grant-layout">
      <div className="terminal-panel">
        <div className="terminal-title">
          <span />
          <span />
          <span />
        </div>
        <pre>{`$ yolobox login
browser grant requested
account: ${user?.email || "signed-in user"}
code: ${formatUserCode(userCode)}`}</pre>
      </div>
      <div className="auth-panel grant-panel">
        <div className="grant-icon"><ShieldCheck size={28} /></div>
        <div>
          <span className="eyebrow">CLI access request</span>
          <h1>Allow yolobox CLI?</h1>
          <p>Grant this terminal session access to your machines as <strong>{user?.email || "this account"}</strong>.</p>
        </div>
        <div className="code-chip">{formatUserCode(userCode)}</div>
        {verify.error ? <p className="error">{(verify.error as Error).message}</p> : null}
        {approve.isSuccess ? <p className="success-text">Access granted. You can return to the terminal.</p> : null}
        {deny.isSuccess ? <p className="error">Request denied. You can return to the terminal.</p> : null}
        <div className="grant-actions">
          {finished ? (
            <button className="primary-button" type="button" onClick={onDone}>
              <Check size={16} />
              Done
            </button>
          ) : (
            <>
              <button className="primary-button" type="button" onClick={() => approve.mutate()} disabled={blocked}>
                <Check size={16} />
                Allow
              </button>
              <button className="danger-button" type="button" onClick={() => deny.mutate()} disabled={blocked}>
                <XCircle size={16} />
                Deny
              </button>
            </>
          )}
        </div>
      </div>
    </section>
  );
}

function Dashboard({ token, user }: { token: string; user?: AuthUser }) {
  const [name, setName] = useState("");
  const [selected, setSelected] = useState<string>("");
  const queryClient = useQueryClient();
  const providers = useQuery({
    queryKey: ["providers"],
    queryFn: () => apiFetch<ProvidersResponse>("/v1/providers", token),
  });
  const machines = useQuery({
    queryKey: ["machines", token],
    queryFn: () => apiFetch<MachinesResponse>("/v1/machines", token),
    refetchInterval: 15000,
  });
  const createMachine = useMutation({
    mutationFn: () => apiFetch<MachineResponse>("/v1/machines", token, { method: "POST", body: { name } }),
    onSuccess: (data) => {
      setName("");
      setSelected(data.machine.name);
      void queryClient.invalidateQueries({ queryKey: ["machines", token] });
    },
  });
  const destroyMachine = useMutation({
    mutationFn: (machineName: string) => apiFetch(`/v1/machines/${encodeURIComponent(machineName)}`, token, { method: "DELETE" }),
    onSuccess: () => {
      setSelected("");
      void queryClient.invalidateQueries({ queryKey: ["machines", token] });
    },
  });
  const machineList = useMemo(() => [...(machines.data?.machines || [])].sort((a, b) => a.name.localeCompare(b.name)), [machines.data]);
  const selectedMachine = machineList.find((machine) => machine.name === selected) || machineList[0];
  const connect = useQuery({
    queryKey: ["connect", selectedMachine?.name, token],
    enabled: Boolean(selectedMachine),
    queryFn: () => apiFetch<ConnectResponse>(`/v1/machines/${encodeURIComponent(selectedMachine?.name || "")}/connect`, token),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    createMachine.mutate();
  }

  return (
    <section className="dashboard">
      <div className="rail">
        <div className="account">
          <span>signed in</span>
          <strong>{user?.email || "account"}</strong>
        </div>
        <form className="create-form" onSubmit={submit}>
          <label>
            Machine name
            <input value={name} onChange={(event) => setName(slugName(event.target.value))} placeholder="main-dev" required />
          </label>
          <button className="primary-button" type="submit" disabled={createMachine.isPending}>
            <Plus size={16} />
            {createMachine.isPending ? "Creating" : "Create"}
          </button>
          {createMachine.error ? <p className="error">{(createMachine.error as Error).message}</p> : null}
        </form>
        <div className="provider-strip">
          {(providers.data?.providers || []).map((provider) => (
            <div className="provider-pill" key={provider.name}>
              <Cloud size={16} />
              <span>{provider.label}</span>
            </div>
          ))}
        </div>
      </div>

      <div className="machine-table">
        <div className="section-heading">
          <div>
            <span>machines</span>
            <strong>{machineList.length}</strong>
          </div>
          <button className="icon-button" type="button" onClick={() => void machines.refetch()} title="Refresh">
            <RefreshCw size={16} />
          </button>
        </div>
        <div className="rows">
          {machineList.map((machine) => (
            <button className={`machine-row ${machine.name === selectedMachine?.name ? "selected" : ""}`} type="button" key={machine.name} onClick={() => setSelected(machine.name)}>
              <span className="machine-status"><MonitorDot size={16} /></span>
              <span>
                <strong>{machine.name}</strong>
                <small>{machine.provider_label || machine.provider || "provider"} / {machine.region || "region"}</small>
              </span>
              <code>{machine.preview_hostname || machine.public_ipv4 || "pending"}</code>
            </button>
          ))}
          {!machineList.length ? <div className="empty">No machines yet.</div> : null}
        </div>
      </div>

      <MachineDetail
        machine={selectedMachine}
        connect={connect.data}
        loading={machines.isLoading || connect.isLoading}
        onDestroy={(machineName) => destroyMachine.mutate(machineName)}
        destroying={destroyMachine.isPending}
      />
    </section>
  );
}

function MachineDetail({ machine, connect, loading, onDestroy, destroying }: {
  machine?: Machine;
  connect?: ConnectResponse;
  loading: boolean;
  onDestroy: (name: string) => void;
  destroying: boolean;
}) {
  if (!machine) {
    return (
      <div className="detail empty-detail">
        <Server size={32} />
        <span>{loading ? "Loading machines" : "Create or select a machine"}</span>
      </div>
    );
  }
  return (
    <div className="detail">
      <div className="detail-header">
        <div>
          <span>{connect?.status || "machine"}</span>
          <h1>{machine.name}</h1>
        </div>
        <button className="danger-button" type="button" onClick={() => onDestroy(machine.name)} disabled={destroying} title="Destroy machine">
          <Trash2 size={16} />
          Destroy
        </button>
      </div>
      <div className="metrics">
        <Metric label="Provider" value={machine.provider_label || machine.provider || "-"} />
        <Metric label="Region" value={machine.region || "-"} />
        <Metric label="Size" value={machine.size || "-"} />
        <Metric label="Image" value={machine.image || "-"} />
      </div>
      <CommandBlock label="SSH" value={connect?.connect.ssh || sshFallback(machine)} />
      <CommandBlock label="Preview URL" value={machine.preview_url || ""} />
      <CommandBlock label="CLI connect" value={connect?.connect.cli || `yolobox remote connect ${machine.name}`} />
      <CommandBlock label="CLI run/sync" value={connect?.connect.cli_run || `yolobox remote --name ${machine.name}`} />
      <dl className="meta">
        <div><dt>Provider ID</dt><dd>{machine.provider_id || "-"}</dd></div>
        <div><dt>Project path</dt><dd>{machine.project_path || "/opt/yolobox/project"}</dd></div>
        <div><dt>Repo</dt><dd>{machine.repo_url || "-"}</dd></div>
        <div><dt>Branch</dt><dd>{machine.branch || "-"}</dd></div>
        <div><dt>Last sync</dt><dd>{formatDate(machine.last_synced_at)}</dd></div>
        <div><dt>Updated</dt><dd>{formatDate(machine.updated_at)}</dd></div>
      </dl>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function CommandBlock({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="command-block">
      <span>{label}</span>
      <code>{value || "-"}</code>
      <button className="icon-button" type="button" title="Copy" onClick={() => {
        void navigator.clipboard.writeText(value);
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1200);
      }}>
        <Copy size={15} />
      </button>
      {copied ? <em>copied</em> : null}
    </div>
  );
}

async function apiFetch<T = unknown>(path: string, token = "", init: { method?: string; body?: unknown } = {}): Promise<T> {
  const response = await fetch(`${apiBaseURL}${path}`, {
    method: init.method || "GET",
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init.body !== undefined ? { "Content-Type": "application/json" } : {}),
    },
    body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
  });
  if (!response.ok) {
    const detail = await response.text();
    throw new Error(readError(detail) || response.statusText);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

function readError(detail: string): string {
  try {
    const parsed = JSON.parse(detail);
    return parsed.message || parsed.error_description || parsed.error || detail;
  } catch {
    return detail;
  }
}

function slugName(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9-]+/g, "-").replace(/^-+/, "").slice(0, 63);
}

function sshFallback(machine: Machine): string {
  return machine.public_ipv4 ? `ssh ${machine.ssh_user || "root"}@${machine.public_ipv4}` : "";
}

function formatDate(value?: string): string {
  if (!value) return "-";
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function readDeviceUserCode(): string {
  const params = new URLSearchParams(window.location.search);
  return (params.get("user_code") || params.get("code") || "").trim();
}

function formatUserCode(code: string): string {
  const clean = code.trim().replace(/-/g, "");
  if (clean.length === 8) return `${clean.slice(0, 4)}-${clean.slice(4)}`;
  return code.trim();
}

createRoot(document.getElementById("root") as HTMLElement).render(
  <QueryClientProvider client={queryClient}>
    <RouterProvider router={router} />
  </QueryClientProvider>,
);
