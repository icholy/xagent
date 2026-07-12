import { useCallback, useMemo, useState } from 'react'
import { useOrgId } from './use-org-id'

/**
 * Like useLocalStorage, but scoped to the current org.
 * The localStorage key is suffixed with the org ID so each org gets its own value.
 * Empty values are ignored to prevent org switches from clearing saved state.
 */
export function useOrgLocalStorage(
  key: string,
  defaultValue: string,
): [string, (value: string) => void] {
  const orgId = useOrgId()
  const orgKey = `${key}-${orgId}`

  const [prevOrgKey, setPrevOrgKey] = useState(orgKey)
  const [value, setValue] = useState(() => localStorage.getItem(orgKey) ?? defaultValue)

  if (prevOrgKey !== orgKey) {
    setPrevOrgKey(orgKey)
    setValue(localStorage.getItem(orgKey) ?? defaultValue)
  }

  const set = useCallback(
    (v: string) => {
      if (v) {
        localStorage.setItem(orgKey, v)
        setValue(v)
      }
    },
    [orgKey],
  )

  return [value, set]
}

/**
 * Like useOrgLocalStorage, but stores a boolean.
 * The value is persisted as the string "true"/"false" under the org-scoped key.
 */
export function useOrgLocalStorageBoolean(
  key: string,
  defaultValue: boolean,
): [boolean, (value: boolean) => void] {
  const [value, setValue] = useOrgLocalStorage(key, String(defaultValue))

  const set = useCallback((v: boolean) => setValue(String(v)), [setValue])

  return useMemo(() => [value === 'true', set], [value, set])
}
