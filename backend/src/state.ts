import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { BackendState, RemoteMachine, stateVersion } from "./types.js";

export class StateStore {
  private state: BackendState | undefined;

  constructor(
    private readonly path: string,
    private readonly provider: string,
  ) {}

  async load(): Promise<BackendState> {
    if (this.state) return this.snapshot();
    this.state = {
      version: stateVersion,
      provider: this.provider,
      machines: {},
    };
    try {
      const data = await readFile(this.path, "utf8");
      if (data.trim() !== "") {
        const parsed = JSON.parse(data) as BackendState;
        this.state = {
          version: parsed.version || stateVersion,
          provider: parsed.provider || this.provider,
          machines: parsed.machines || {},
          updated_at: parsed.updated_at,
        };
      }
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
    return this.snapshot();
  }

  async listMachines(): Promise<RemoteMachine[]> {
    const state = await this.load();
    return Object.values(state.machines);
  }

  async getMachine(name: string): Promise<RemoteMachine | undefined> {
    const state = await this.load();
    return state.machines[name];
  }

  async putMachine(machine: RemoteMachine): Promise<void> {
    await this.update((state) => {
      state.machines[machine.name] = machine;
    });
  }

  async patchMachine(name: string, patch: RemoteMachine): Promise<RemoteMachine> {
    let updated: RemoteMachine | undefined;
    await this.update((state) => {
      const existing = state.machines[name];
      if (!existing) return;
      updated = {
        ...existing,
        ...patch,
        name,
        updated_at: new Date().toISOString(),
      };
      state.machines[name] = updated;
    });
    if (!updated) throw new Error("machine is not leased");
    return updated;
  }

  async deleteMachine(name: string): Promise<void> {
    await this.update((state) => {
      delete state.machines[name];
    });
  }

  private async update(fn: (state: BackendState) => void): Promise<void> {
    const state = await this.load();
    fn(state);
    state.version = stateVersion;
    state.updated_at = new Date().toISOString();
    await mkdir(dirname(this.path), { recursive: true });
    await writeFile(this.path, `${JSON.stringify(state, null, 2)}\n`);
    this.state = state;
  }

  private snapshot(): BackendState {
    if (!this.state) {
      return { version: stateVersion, provider: this.provider, machines: {} };
    }
    return {
      ...this.state,
      machines: { ...this.state.machines },
    };
  }
}
