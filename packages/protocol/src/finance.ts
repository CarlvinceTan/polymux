/** Finance never transports bank login credentials or card numbers. */
export interface FinanceAccountDto {
  id: string;
  name: string;
  bank: string;
  currency: string;
  booked: number | null;
  available: number | null;
  consentUntil: string | null;
  balanceDate: string | null;
  error: string | null;
}
export interface FinanceTransactionDto {
  id: string;
  date: string;
  description: string;
  amount: number;
  currency: string;
  status: string;
}
export interface FinanceReadRequest {
  serverId: string;
  accountId?: string;
  continuation?: string;
}
export interface FinanceReadDto {
  accounts: FinanceAccountDto[];
  transactions: FinanceTransactionDto[];
  continuation: string | null;
  fetchedAt: string;
}
