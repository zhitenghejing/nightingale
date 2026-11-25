import { RequestMethod } from '@/store/common';
export type ConsoleProtocol = 'ssh' | 'rdp';
export interface CredentialInput {
  name: string;
  group_id: number;
  username?: string;
  password?: string;
  disabled?: boolean;
  record_version?: number;
}
export interface Endpoint {
  host: string;
  port: number;
  fingerprint: string;
  credential_id: string;
  new_credential?: CredentialInput;
}
export interface Connection {
  protocol?: ConsoleProtocol;
  ident?: string;
  configured?: boolean;
  stale?: boolean;
  group_id: number;
  enabled: boolean;
  target: Endpoint;
  jump?: Endpoint;
  log_paths: string[];
  record_version: number;
}
export interface Features {
  enabled: boolean;
  configure: boolean;
  manage_credentials: boolean;
  execute: boolean;
  terminal: boolean;
  rdp?: boolean;
}
export const api = async <T = any>(path: string, method = RequestMethod.Get, data?: unknown): Promise<T> => {
  const { default: request } = await import('@/utils/request');
  return request(`/api/n9e/remote-access/${path}`, { method, data, silence: true }).then((r) => r.dat);
};
export const connectionPath = (ident: string) => `connection?ident=${encodeURIComponent(ident)}`;
