// 内嵌 Tailscale 节点的状态展示规则：决定状态文案，以及是否需要给出登录链接。

export interface ITailscaleStatus {
    state: string;
    authURL: string;
    ips: string[] | null;
    error: string;
    keyExpiry?: string;
}

export interface ITailscaleStatusView {
    text: string;
    loginURL: string;
}

export interface ITailscaleLanguages {
    tailscaleRunning: string;
    tailscaleNeedsLogin: string;
    tailscaleStarting: string;
    tailscaleError: string;
    tailscaleDisabled: string;
    tailscaleWaitingAuth: string;
    tailscaleOpenLogin: string;
    tailscaleNeedsMachineAuth: string;
    tailscaleKeyExpired: string;
    tailscaleKeyExpiring: string;
}

const keyExpiryWarningWindow = 7 * 24 * 60 * 60 * 1000;

const parseKeyExpiry = (value?: string): Date | null => {
    if (!value) {
        return null;
    }
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? null : date;
};

const formatKeyExpiryDate = (date: Date): string => date.toISOString().slice(0, 10);

/**
 * 计算状态文案与登录链接。
 *
 * 已连接时文案附带 Tailnet 地址，且不再给出登录链接与错误；未连接时若有控制面错误，额外附上真实原因，
 * 等待登录类状态在没有认证地址时追加等待提示。密钥过期与即将过期各自给出提示。
 */
export const describeTailscaleStatus = (status: ITailscaleStatus, languages: ITailscaleLanguages,
                                        now = Date.now()): ITailscaleStatusView => {
    const keyExpiry = parseKeyExpiry(status.keyExpiry);
    if ("running" === status.state) {
        const ips = status.ips?.length ? ` · ${status.ips.join(", ")}` : "";
        let text = `${languages.tailscaleRunning}${ips}`;
        if (keyExpiry && keyExpiry.getTime() - now <= keyExpiryWarningWindow) {
            text += ` · ${languages.tailscaleKeyExpiring.replace("${date}", formatKeyExpiryDate(keyExpiry))}`;
        }
        return {text, loginURL: ""};
    }
    if ("needsMachineAuth" === status.state) {
        // 机器授权需要管理员在后台批准，重新登录不会解决问题，因此不给登录链接
        return {text: languages.tailscaleNeedsMachineAuth, loginURL: ""};
    }

    const expired = null != keyExpiry && keyExpiry.getTime() <= now;
    let text: string;
    switch (status.state) {
        case "starting":
            text = languages.tailscaleStarting;
            break;
        case "needsLogin":
            text = expired ? languages.tailscaleKeyExpired : languages.tailscaleNeedsLogin;
            break;
        case "error":
            text = languages.tailscaleError;
            break;
        default:
            text = languages.tailscaleDisabled;
            break;
    }

    // 未连接时把控制面失败的真实原因一并展示，避免用户只看一句泛泛的等待提示
    const detail = status.error ? `${languages.tailscaleError}: ${status.error}` : "";
    if ("error" === status.state) {
        // 错误状态直接用真实原因代替泛化文案
        text = detail || text;
    } else if (detail) {
        text = `${text} · ${detail}`;
    }

    if (status.authURL) {
        return {text, loginURL: status.authURL};
    }
    const waitingAuth = "starting" === status.state || ("needsLogin" === status.state && !expired);
    return {text: waitingAuth ? `${text} · ${languages.tailscaleWaitingAuth}` : text, loginURL: ""};
};

// 先覆盖文案再按需追加链接，重复调用不会累积旧节点。
//
// 文案与链接都未变化时直接返回，避免轮询反复重建链接节点导致点击落空、焦点丢失。
export const applyTailscaleStatus = (element: HTMLElement, status: ITailscaleStatus,
                                    languages: ITailscaleLanguages): void => {
    const view = describeTailscaleStatus(status, languages);
    const existingLink = element.querySelector("a");
    const currentHref = existingLink ? existingLink.getAttribute("href") : "";
    if (view.text === element.firstChild?.nodeValue && view.loginURL === currentHref) {
        return;
    }
    element.textContent = view.text;
    if (!view.loginURL) {
        return;
    }
    const link = document.createElement("a");
    link.className = "fn__a";
    link.href = view.loginURL;
    link.target = "_blank";
    link.rel = "noreferrer";
    link.textContent = languages.tailscaleOpenLogin;
    element.appendChild(document.createTextNode(" "));
    element.appendChild(link);
};
