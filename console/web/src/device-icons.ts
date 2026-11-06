export const deviceIconOptions = [
  { value: "auto", label: "自动识别" },
  { value: "computer", label: "电脑" },
  { value: "windows", label: "Windows" },
  { value: "apple", label: "Apple" },
  { value: "android", label: "Android" },
  { value: "linux", label: "Linux" },
  { value: "docker", label: "Docker" },
  { value: "freebsd", label: "FreeBSD" },
  { value: "openwrt", label: "OpenWrt" },
  { value: "nas", label: "NAS" },
  { value: "phone", label: "手机" },
];

export function resolveDeviceIcon(icon: string | undefined, name: string, tags: string[] = []) {
  if (icon && icon !== "auto" && deviceIconOptions.some(option => option.value === icon)) return icon;
  if (/openwrt|immortalwrt|istoreos/i.test(name) || tags.includes("网关")) return "openwrt";
  if (/docker/i.test(name)) return "docker";
  if (/synology|qnap|asustor|\bnas\b|群晖|威联通/i.test(name)) return "nas";
  if (/android|安卓/i.test(name)) return "android";
  if (/iphone|ipad|macbook|imac|mac mini|apple/i.test(name)) return "apple";
  if (/windows|win10|win11/i.test(name)) return "windows";
  if (/freebsd/i.test(name)) return "freebsd";
  if (/linux|debian|ubuntu|centos|fedora/i.test(name)) return "linux";
  return "computer";
}
