// Atlas Core (open source) or Pro. The Pro screens live only in the private repo, so Core must never link to them.
const IS_PRO = process.env.NEXT_PUBLIC_ATLAS_EDITION === "pro";
const PRO_ONLY_PATHS = ["/dashboard", "/alerts", "/map", "/audit"];

export const HOME_PATH = IS_PRO ? "/dashboard" : "/ask";
/** False for screens this build does not include. */
export const hasScreen = (href: string) => IS_PRO || !PRO_ONLY_PATHS.some((p) => href === p || href.startsWith(`${p}?`) || href.startsWith(`${p}/`));
