const getAppleSiliconDownloadURL = (version) =>
    `https://github.com/ulquiola/siyuan/releases/download/v${version}/siyuan-${version}-mac-arm64.dmg`;

const shouldDownloadAppleSilicon = (response) => response === 0;

const shouldShowAppleSiliconWarning = ({
    isDevelopment,
    isPackaged,
    platform,
    runningUnderARM64Translation,
    simulateRosetta,
}) => {
    if (!isPackaged && isDevelopment && simulateRosetta) {
        return true;
    }
    return isPackaged && platform === "darwin" && runningUnderARM64Translation;
};

module.exports = {
    getAppleSiliconDownloadURL,
    shouldDownloadAppleSilicon,
    shouldShowAppleSiliconWarning,
};
