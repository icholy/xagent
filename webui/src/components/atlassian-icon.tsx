import type { SVGProps } from 'react'

// Atlassian brand mark. lucide-react removed brand icons in v1, so we inline the
// official logo here. The path is the canonical Atlassian mark from Simple Icons
// (https://github.com/simple-icons/simple-icons, CC0). Sizing/colour follow the
// same `className` convention as lucide icons (e.g. "h-4 w-4"); fill uses
// currentColor.
export function AtlassianIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" {...props}>
      <path d="M7.12 11.084a.683.683 0 00-1.16.126L.075 22.974a.703.703 0 00.63 1.018h8.19a.678.678 0 00.63-.39c1.767-3.65.696-9.203-2.406-12.52zM11.434.386a15.515 15.515 0 00-.906 15.317l3.95 7.9a.703.703 0 00.628.388h8.19a.703.703 0 00.63-1.017L12.63.38a.664.664 0 00-1.196.006z" />
    </svg>
  )
}
