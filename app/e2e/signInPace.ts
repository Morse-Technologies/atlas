// El servidor admite SIGN_IN_LIMIT accesos por minuto y dirección (ventana fija). Quedarse por debajo en cualquier minuto
// deslizante basta, y el margen de reloj cubre la diferencia entre el reloj de la prueba y el del servidor.
export const SIGN_IN_LIMIT = 10;
export const SIGN_IN_WINDOW_MS = 60_000;
export const CLOCK_MARGIN_MS = 1_000;

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Anota los intentos de acceso y espera cuando habría demasiados en el último minuto, con la misma idea que
 * `freshTotp`. Deja un hueco libre para el acceso que la propia prueba no controla (p. ej. el del asistente de configuración).
 */
export function createSignInPace(clock = Date.now, pause = sleep) {
  const attempts: number[] = [];
  const forgetOld = () => {
    while (attempts.length && clock() - attempts[0] >= SIGN_IN_WINDOW_MS + CLOCK_MARGIN_MS) attempts.shift();
  };
  return async function waitForSignInSlot(): Promise<void> {
    forgetOld();
    while (attempts.length >= SIGN_IN_LIMIT - 1) {
      await pause(attempts[0] + SIGN_IN_WINDOW_MS + CLOCK_MARGIN_MS - clock());
      forgetOld();
    }
    attempts.push(clock());
  };
}

/** The one pace shared by the whole journey: one Playwright worker, one serial run. */
export const waitForSignInSlot = createSignInPace();
