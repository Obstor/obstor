"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { BucketModal } from "./BucketModal";

export function CreateBucketButton() {
  const router = useRouter();
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex shrink-0 items-center gap-1.5 rounded-md bg-amber-500 px-3 py-1.5 font-body font-medium text-black text-xs transition-colors hover:bg-amber-400"
      >
        <span className="icon-[tabler--plus] block text-[13px]" />
        Create bucket
      </button>
      <BucketModal
        open={open}
        onClose={() => setOpen(false)}
        onSuccess={() => router.refresh()}
        editBucket={null}
      />
    </>
  );
}
