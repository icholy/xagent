import { defaultParseSearch, defaultStringifySearch } from '@tanstack/react-router'

// TanStack Router only exposes the two global hooks (parseSearch /
// stringifySearch) plus per-route validateSearch (parse-side only); there is no
// built-in per-key codec for the stringify side. These helpers let us register
// per-key overrides and delegate every other key to the TanStack defaults.
//
// The defaults JSON-parse and JSON-stringify each value, so a bare ?org=1
// parses to the number 1 and a string "1" stringifies to the quoted ?org=%221%22.
// Registering `org: String` keeps org a string on parse and emits it bare on
// stringify, producing clean ?org=1 URLs.

export function createParseSearch(parsers: Record<string, (value: unknown) => unknown>) {
  return (searchStr: string) => {
    const parsed = defaultParseSearch(searchStr) as Record<string, unknown>
    for (const key in parsers) {
      if (key in parsed) {
        parsed[key] = parsers[key](parsed[key])
      }
    }
    return parsed
  }
}

export function createStringifySearch(stringifiers: Record<string, (value: unknown) => string>) {
  return (search: Record<string, unknown>) => {
    const rest = { ...search }
    const parts: string[] = []
    for (const key in stringifiers) {
      if (search[key] != null) {
        parts.push(
          `${encodeURIComponent(key)}=${encodeURIComponent(stringifiers[key](search[key]))}`,
        )
        delete rest[key]
      }
    }
    let out = defaultStringifySearch(rest)
    for (const part of parts) {
      out += out ? `&${part}` : `?${part}`
    }
    return out
  }
}
