// "Safe triangle" hover intent (the Amazon mega-menu technique): while the pointer travels from the trigger
// toward a flyout, it may cross other triggers. As long as it stays inside the triangle formed by where it
// left the trigger and the flyout's near edge, the flyout stays open.

export type Pt = { x: number; y: number };

const sign = (p: Pt, a: Pt, b: Pt) => (p.x - b.x) * (a.y - b.y) - (a.x - b.x) * (p.y - b.y);

type Triangle = [Pt, Pt, Pt];

function inTriangle(p: Pt, [a, b, c]: Triangle): boolean {
  const d1 = sign(p, a, b), d2 = sign(p, b, c), d3 = sign(p, c, a);
  const neg = d1 < 0 || d2 < 0 || d3 < 0;
  const pos = d1 > 0 || d2 > 0 || d3 > 0;
  return !(neg && pos);
}

export const inRect = (p: Pt, r: DOMRect, pad = 0) =>
  p.x >= r.left - pad && p.x <= r.right + pad && p.y >= r.top - pad && p.y <= r.bottom + pad;

const EDGE_SLACK_PX = 8; // widens the target so a slightly high or low path still counts

/** True while the pointer is on its way from `leave` to a flyout whose left edge is `target`. */
export function onTheWay(p: Pt, leave: Pt, target: DOMRect): boolean {
  return inTriangle(p, [leave, { x: target.left, y: target.top - EDGE_SLACK_PX }, { x: target.left, y: target.bottom + EDGE_SLACK_PX }]);
}

// Self-check: a point between the leave point and the flyout's edge is inside; one behind it is not.
console.assert(inTriangle({ x: 5, y: 5 }, [{ x: 0, y: 5 }, { x: 10, y: 0 }, { x: 10, y: 10 }]), "safe triangle: inside");
console.assert(!inTriangle({ x: -2, y: 5 }, [{ x: 0, y: 5 }, { x: 10, y: 0 }, { x: 10, y: 10 }]), "safe triangle: outside");
