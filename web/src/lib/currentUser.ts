import { useOutletContext } from "react-router";
import type { Me } from "./session";

export function useCurrentUser(): Me {
    return useOutletContext<Me>();
}
