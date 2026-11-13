import { expect, test } from "@playwright/test";
import { CLOCK_MARGIN_MS, SIGN_IN_LIMIT, SIGN_IN_WINDOW_MS, createSignInPace } from "./signInPace";

const START = 1_000_000_000_000;
const SPARE_SLOT = 1;

/** A pace with a clock that only moves when the pace waits. */
function paceWithFakeClock() {
  const clock = { now: START, waits: [] as number[] };
  const pause = async (ms: number) => { clock.waits.push(ms); clock.now += ms; };
  return { clock, waitForSignInSlot: createSignInPace(() => clock.now, pause) };
}

test("the first attempts below the limit do not wait", async () => {
  const { clock, waitForSignInSlot } = paceWithFakeClock();
  for (let n = 0; n < SIGN_IN_LIMIT - SPARE_SLOT; n++) await waitForSignInSlot();
  expect(clock.waits).toEqual([]);
});

test("the attempt that would leave no spare slot waits until the first one leaves the window", async () => {
  const { clock, waitForSignInSlot } = paceWithFakeClock();
  for (let n = 0; n < SIGN_IN_LIMIT - SPARE_SLOT; n++) await waitForSignInSlot();
  await waitForSignInSlot();
  expect(clock.now).toBe(START + SIGN_IN_WINDOW_MS + CLOCK_MARGIN_MS);
});

test("the wait asked for is exactly the time that is missing", async () => {
  const { clock, waitForSignInSlot } = paceWithFakeClock();
  await waitForSignInSlot();
  clock.now += 20_000;
  for (let n = 1; n < SIGN_IN_LIMIT - SPARE_SLOT; n++) await waitForSignInSlot();
  await waitForSignInSlot();
  expect(clock.waits).toEqual([SIGN_IN_WINDOW_MS + CLOCK_MARGIN_MS - 20_000]);
});

test("attempts older than the window do not count", async () => {
  const { clock, waitForSignInSlot } = paceWithFakeClock();
  for (let n = 0; n < SIGN_IN_LIMIT - SPARE_SLOT; n++) await waitForSignInSlot();
  clock.now += SIGN_IN_WINDOW_MS + CLOCK_MARGIN_MS;
  await waitForSignInSlot();
  expect(clock.waits).toEqual([]);
});
