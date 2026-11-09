import { Suspense } from "react";
import { AskScreen } from "@/features/ask/AskScreen";

export const metadata = { title: "Assistant" };

export default function Page() {
  return (
    <Suspense>
      <AskScreen />
    </Suspense>
  );
}
