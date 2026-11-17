import { useSuspenseQuery } from "@tanstack/react-query";
import { queries } from "../../services/queries";
import { Heading } from "../../components/Heading";
import { AuditTable } from "../../components/Access";
export function AuditView() {
  const { data } = useSuspenseQuery(queries.audit());
  return (
    <>
      <Heading
        kicker="Platform"
        title="Audit log"
        subtitle="Every change to users, roles, members and overrides, with who made it."
      />
      <AuditTable entries={data} />
    </>
  );
}
