defmodule Consumer.MixProject do
  use Mix.Project

  def project do
    [app: :consumer, version: "1.0.0", deps: deps()]
  end

  # keep this list and comment
  defp deps do
    [
      {:jason, "~> 1.4"},
    # git-a2a:begin acme_lib_utils
    {:acme_lib_utils, git: "https://github.com/acme/lib-utils.git", ref: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", sparse: "elixir/lib"},
    # git-a2a:end acme_lib_utils
    ]
  end
end
