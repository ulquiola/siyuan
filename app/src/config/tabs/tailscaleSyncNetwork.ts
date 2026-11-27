export type TailscaleSyncNetwork = "direct" | "proxy";

/**
 * 把后端配置收敛到两个已知值；旧配置、空值和未知值一律按直连处理。
 */
export const normalizeSyncNetwork = (value: unknown): TailscaleSyncNetwork => "proxy" === value ? "proxy" : "direct";