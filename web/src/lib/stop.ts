import { useCallback, useEffect, useRef, useState } from 'react'
import { usePanel } from './panel'

// useStopArming makes stopping the Companion take two presses within 3 seconds:
// the first arms it, the second stops it.
export function useStopArming() {
  const panel = usePanel()
  const [armed, setArmed] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout>>()
  useEffect(() => () => clearTimeout(timer.current), [])
  const press = useCallback(() => {
    clearTimeout(timer.current)
    if (!armed) {
      setArmed(true)
      timer.current = setTimeout(() => setArmed(false), 3000)
      return false
    }
    setArmed(false)
    void panel.stopCompanion()
    return true
  }, [armed, panel])
  return { armed, press }
}
