fn main() -> Result<(), Box<dyn std::error::Error>> {
    // NOTE: prost only tracks the files it compiles, not what they import,
    // so editing common.proto used to leave stale generated code behind
    println!("cargo:rerun-if-changed=../../../spec/proto");
    tonic_prost_build::configure()
        // NOTE: the generated transport helper `AgentClient::connect` clashes with
        // the `Connect` rpc, so we build our own Channel and use `::new`
        .build_transport(false)
        .compile_protos(
            &[
                "../../../spec/proto/seismo/v1/agent.proto",
                "../../../spec/proto/seismo/v1/coordinator.proto",
            ],
            &["../../../spec/proto"],
        )?;
    Ok(())
}
