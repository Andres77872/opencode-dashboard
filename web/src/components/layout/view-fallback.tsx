import { ViewSkeleton } from '../vael/feedback'

/* Shown while a route's code chunk is in flight. It is the same skeleton the
   views render while their data loads, so arriving at a view looks like one
   continuous load rather than two. */
export function ViewFallback() {
  return <ViewSkeleton label="Loading view" />
}
