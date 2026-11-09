import { Suspense } from "react";
import { AskScreen } from "@/features/ask/AskScreen";

export const metadata = { title: "Assistant" };

async function Conversation({ params }: { params: PageProps<"/ask/[id]">["params"] }) {
  const { id } = await params;
  return <AskScreen key={id} conversationId={id} />;
}

export default function Page({ params }: PageProps<"/ask/[id]">) {
  return (
    <Suspense>
      <Conversation params={params} />
    </Suspense>
  );
}
