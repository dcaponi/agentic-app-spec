export interface AddAgentOptions {
    type: 'llm' | 'deterministic';
    model: string;
    inputType: 'image' | 'text';
}
export declare function runAddAgent(id: string, opts: AddAgentOptions): void;
//# sourceMappingURL=add-agent.d.ts.map