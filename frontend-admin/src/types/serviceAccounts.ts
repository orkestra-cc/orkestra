export interface ServiceAccount {
  id: string;
  name: string;
  email: string;
  isActive: boolean;
  activeCredentials: number;
  createdAt: string;
}
export interface ServiceAccountCredential {
  id: string;
  clientId: string;
  label: string;
  createdAt: string;
  lastUsedAt?: string;
  revokedAt?: string;
}
export interface ServiceAccountDetail extends ServiceAccount {
  credentials: ServiceAccountCredential[];
}
export interface ServiceAccountWithSecret extends ServiceAccount {
  clientId: string;
  clientSecret: string;
}
export interface CredentialWithSecret extends ServiceAccountCredential {
  clientSecret: string;
}
