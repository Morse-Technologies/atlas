import { Suspense } from "react";
import { DocumentScreen } from "@/features/documents/DocumentsScreens";

export const metadata = { title: "Document" };

async function Viewer({ params }: { params: PageProps<"/documents/[id]">["params"] }) {
  const { id } = await params;
  return <DocumentScreen key={id} id={id} />;
}

export default function Page({ params }: PageProps<"/documents/[id]">) {
  return (
    <Suspense>
      <Viewer params={params} />
    </Suspense>
  );
}
