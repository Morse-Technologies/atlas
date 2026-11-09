import { Suspense } from "react";
import { LoginScreen } from "@/features/access/AccessScreens";

export const metadata = { title: "Sign in" };

export default function Page() {
  return (
    <Suspense>
      <LoginScreen />
    </Suspense>
  );
}
