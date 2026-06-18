import { useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { exportMyData, requestErasure } from "@/api/dsr";

// AccountDsrPage is the Tier-2 self-service surface for GDPR data-subject
// rights: download a copy of your personal data (Art. 15 / 20) and lodge a
// right-to-erasure request (Art. 17) that an operator reviews.
export function AccountDsrPage() {
  const [reason, setReason] = useState("");

  const exportMut = useMutation({
    mutationFn: exportMyData,
    onSuccess: data => {
      const blob = new Blob([JSON.stringify(data, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "my-data-export.json";
      a.click();
      URL.revokeObjectURL(url);
    },
  });

  const erasureMut = useMutation({
    mutationFn: () => requestErasure(reason || undefined),
  });

  return (
    <section className="mx-auto max-w-2xl space-y-8 p-6">
      <header>
        <h1 className="text-2xl font-semibold">Privacy &amp; my data</h1>
        <p className="text-gray-500">
          Exercise your GDPR rights over the personal data we hold about you.
        </p>
      </header>

      <div className="space-y-3 rounded-lg border p-4">
        <h2 className="font-medium">Download my data</h2>
        <p className="text-sm text-gray-500">
          Get a machine-readable copy of all personal data associated with your
          account (Art. 15 / 20).
        </p>
        <button
          type="button"
          className="rounded bg-blue-600 px-4 py-2 text-white disabled:opacity-50"
          onClick={() => exportMut.mutate()}
          disabled={exportMut.isPending}
        >
          {exportMut.isPending ? "Preparing…" : "Download my data"}
        </button>
        {exportMut.isError && (
          <p className="text-sm text-red-600">Export failed. Please try again.</p>
        )}
      </div>

      <div className="space-y-3 rounded-lg border p-4">
        <h2 className="font-medium">Request erasure</h2>
        <p className="text-sm text-gray-500">
          Ask us to erase your personal data (Art. 17). An operator reviews the
          request before it is carried out.
        </p>
        <textarea
          className="w-full rounded border p-2 text-sm"
          placeholder="Reason (optional)"
          value={reason}
          onChange={e => setReason(e.target.value)}
        />
        <button
          type="button"
          className="rounded bg-red-600 px-4 py-2 text-white disabled:opacity-50"
          onClick={() => erasureMut.mutate()}
          disabled={erasureMut.isPending || erasureMut.isSuccess}
        >
          {erasureMut.isSuccess
            ? "Request submitted"
            : erasureMut.isPending
              ? "Submitting…"
              : "Request erasure"}
        </button>
        {erasureMut.isError && (
          <p className="text-sm text-red-600">Could not submit the request.</p>
        )}
      </div>
    </section>
  );
}
