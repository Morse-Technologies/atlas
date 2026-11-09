import Link from "next/link";
import { Mark } from "@/ui/atoms";

export const metadata = { title: "Privacy" };

const SECTIONS: { title: string; body: string[] }[] = [
  {
    title: "Who is responsible",
    body: [
      "Atlas runs on your organization's own server. Your organization is the data controller and decides who can sign in, which folders are read and how long records are kept.",
      "Morse Technologies provides the software. It does not host your installation and receives no personal data from it unless your organization signs a separate support agreement.",
    ],
  },
  {
    title: "What Atlas stores",
    body: [
      "Account: name, email, roles, a password hash and an encrypted second-factor secret.",
      "Sessions: sign-in time, IP address and browser, to keep you signed in and to detect misuse.",
      "Activity: the questions you ask, the answers and the document passages they cite, your feedback, and admin changes. These form the audit log.",
      "Documents: copies of files from the folders your admin connects, and the text and metadata extracted from them.",
    ],
  },
  {
    title: "Why",
    body: [
      "To answer questions from your documents, to show only what you are allowed to open, and to keep an audit trail your organization can review.",
    ],
  },
  {
    title: "How Atlas uses AI",
    body: [
      "Answers are written by a language model that runs on your organization's server, using only passages from documents you can open. Every answer cites its sources. AI can make mistakes: check the cited page before acting on an answer.",
      "Documents are read with layout and text-recognition models (Docling) that also run locally. Your data is not used to train any model.",
    ],
  },
  {
    title: "Third parties",
    body: [
      "None at run time. Atlas makes no outbound connections: no analytics, no tracking, no external AI services. Its parts (database, document converter, models) run inside your network.",
      "If your organization turns on optional features that contact outside services, it must tell you which ones and why.",
    ],
  },
  {
    title: "How long",
    body: [
      "Questions, answers and audit events are kept for the period your admin sets in Settings (1, 2 or 5 years) and then deleted. Documents are kept while they exist in their source folder.",
    ],
  },
  {
    title: "Cookies",
    body: [
      "One session cookie keeps you signed in. Your browser also remembers display preferences such as theme. Nothing is used for tracking.",
    ],
  },
  {
    title: "Your rights",
    body: [
      "You can ask your organization to see, correct, export or delete your data, or object to its use. Admins can deactivate and delete accounts from Users. You can also complain to your data protection authority.",
    ],
  },
];

/** Privacy notice: public, so people can read it before they sign in. */
export default function Page() {
  return (
    <main id="main" className="mx-auto min-h-dvh max-w-2xl bg-ground px-5 py-10 sm:px-10">
      <Link href="/login" className="flex items-center gap-2 text-text">
        <Mark className="size-7" />
        <span className="text-section font-medium">Atlas</span>
      </Link>
      <h1 className="mt-10 text-title font-medium">Privacy</h1>
      <p className="mt-1 text-body text-muted">What Atlas keeps about you, why, and for how long.</p>
      {SECTIONS.map((s) => (
        <section key={s.title} className="mt-8">
          <h2 className="text-section font-medium">{s.title}</h2>
          {s.body.map((p) => <p key={p} className="mt-2 text-body">{p}</p>)}
        </section>
      ))}
    </main>
  );
}
