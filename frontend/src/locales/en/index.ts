// English catalog. A locale is a directory under src/locales/<code>/ whose
// index.ts default-exports its messages; plugins/i18n.ts discovers it by glob,
// so adding a locale needs no registration (copy this folder, translate).
import { accountsEn } from './accounts'
import { coreEn } from './core'

export default { ...coreEn, ...accountsEn }
