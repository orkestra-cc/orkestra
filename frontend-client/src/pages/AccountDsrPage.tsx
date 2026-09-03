import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { exportMyData, requestErasure } from "@/api/dsr";

// AccountDsrPage is the Tier-2 self-service surface for GDPR data-subject
// rights: download a copy of your personal data (Art. 15 / 20) and lodge a
// right-to-erasure request (Art. 17) that an operator reviews.
//
// All copy resolves through the default i18next namespace under the `dsr`
// key block (en.json / it.json); the export filename below is deliberately
// NOT translated — it is an identifier the user's filesystem sees, not copy.
export function AccountDsrPage() {
  const { t } = useTranslation();
  const [reason, setReason] = useState("");

  const exportMut = useMutation({
    mutationFn: exportMyData,
    onSuccess: (data) => {
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
        <h1 className="text-2xl font-semibold">{t("dsr.title")}</h1>
        <p className="text-gray-500">{t("dsr.subtitle")}</p>
      </header>

      <div className="space-y-3 rounded-lg border p-4">
        <h2 className="font-medium">{t("dsr.export.title")}</h2>
        <p className="text-sm text-gray-500">{t("dsr.export.subtitle")}</p>
        <button
          type="button"
          className="rounded bg-blue-600 px-4 py-2 text-white disabled:opacity-50"
          onClick={() => exportMut.mutate()}
          disabled={exportMut.isPending}
        >
          {exportMut.isPending
            ? t("dsr.export.submitting")
            : t("dsr.export.submit")}
        </button>
        {exportMut.isError && (
          <p className="text-sm text-red-600">{t("dsr.export.error")}</p>
        )}
      </div>

      <div className="space-y-3 rounded-lg border p-4">
        <h2 className="font-medium">{t("dsr.erasure.title")}</h2>
        <p className="text-sm text-gray-500">{t("dsr.erasure.subtitle")}</p>
        <textarea
          className="w-full rounded border p-2 text-sm"
          placeholder={t("dsr.erasure.reasonPlaceholder")}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
        <button
          type="button"
          className="rounded bg-red-600 px-4 py-2 text-white disabled:opacity-50"
          onClick={() => erasureMut.mutate()}
          disabled={erasureMut.isPending || erasureMut.isSuccess}
        >
          {erasureMut.isSuccess
            ? t("dsr.erasure.submitted")
            : erasureMut.isPending
              ? t("dsr.erasure.submitting")
              : t("dsr.erasure.submit")}
        </button>
        {erasureMut.isError && (
          <p className="text-sm text-red-600">{t("dsr.erasure.error")}</p>
        )}
      </div>
    </section>
  );
}
