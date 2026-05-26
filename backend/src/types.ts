export type RemoteMachine = {
  name: string;
  user_id?: string;
  provider_label?: string;
  provider_name?: string;
  provider?: string;
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
  last_command?: string[];
  created_at?: string;
  updated_at?: string;
  last_synced_at?: string;
  bootstrap_complete?: boolean;
};

export type EnsureMachineRequest = {
  name: string;
  provider?: string;
  provider_name?: string;
  tier?: string;
  ssh_user?: string;
  source_path?: string;
  repo_url?: string;
  branch?: string;
};

export type ListProviderMachinesRequest = {
  provider_name_suffix?: string;
  ssh_user?: string;
};

export type MachineProviderInfo = {
  name: string;
  label: string;
  capabilities: Array<"create" | "destroy" | "list" | "connect">;
};

export type MachineProvider = {
  name: string;
  label?: string;
  info?: MachineProviderInfo;
  ensureMachine(request: EnsureMachineRequest): Promise<{ machine: RemoteMachine; status?: string }>;
  getMachine(machine: RemoteMachine): Promise<{ machine: RemoteMachine; status?: string }>;
  listMachines(request: ListProviderMachinesRequest): Promise<Array<{ machine: RemoteMachine; status?: string }>>;
  releaseMachine(machine: RemoteMachine): Promise<void>;
};

export type BackendState = {
  version: number;
  provider: string;
  machines: Record<string, RemoteMachine>;
  updated_at?: string;
};

export const stateVersion = 2;
export const defaultProjectPath = "/opt/yolobox/project";
