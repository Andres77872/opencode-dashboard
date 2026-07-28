/* Chart helpers shared by the Vael chart components. They live outside
   charts.tsx so that file exports only components and stays a valid Fast
   Refresh boundary. */
import { useCallback, useRef, useState } from 'react'

/** Round a value up to a clean axis maximum (1, 2, 5 or 10 times a power of ten). */
export function niceMax(v: number): number {
  if (v <= 0) return 1
  const mag = Math.pow(10, Math.floor(Math.log10(v)))
  const n = v / mag
  const step = n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10
  return step * mag
}

/** Observe an element's width; returns [ref, width]. Attach ref to a block element.
    The ref is a callback ref so the observer follows the element across
    conditional renders — views mount a loading skeleton first, and an effect
    that only checked ref.current once would never see the real chart node. */
export function useWidth(initial = 600): [(node: HTMLDivElement | null) => void, number] {
  const [w, setW] = useState(initial)
  const roRef = useRef<ResizeObserver | null>(null)
  const ref = useCallback((node: HTMLDivElement | null) => {
    roRef.current?.disconnect()
    roRef.current = null
    if (!node || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      const cw = entries[0].contentRect.width
      if (cw) setW(Math.round(cw))
    })
    ro.observe(node)
    roRef.current = ro
  }, [])
  return [ref, w]
}
