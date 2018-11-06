#include <unistd.h>
#include <thread>
#include <string.h>
#include <iostream>
#include <vector>
#include <sstream>

std::string process_name;

void thread_func (void)
{

      cpu_set_t mask;
      CPU_ZERO(&mask);
      int ret = pthread_getaffinity_np(pthread_self(), sizeof(cpu_set_t), &mask);
      if (ret != 0) {
              printf("Error getting thread affinity %d",ret);
      }
      int cpu;
      std::stringstream output;
      output << process_name << " thread(s) running in cpu(s) ";
      for (cpu=0; cpu < CPU_SETSIZE;cpu++) {
              if (CPU_ISSET(cpu,&mask)) output << cpu << ",";
      }
      output.seekp(-1,output.cur);
      output << std::endl;
      std::cout << output.str();
      fflush(stdout);
      while(true);
}

int read_cores(int cores[],char *arg)
{
        int i = 0;
        arg = strtok(arg,",");
        while (arg != NULL) {
                cores[i++]=atoi(arg);
                arg = strtok(NULL,",");
        }
        return i;
}

int main(int argc, char* argv[])
{
        int opt,cores[10],num_cores=0,i;

        while ((opt = getopt (argc, argv, "cn::")) != -1) {
                switch (opt)
                {
                case 'c': {
                        num_cores = read_cores(cores,argv[optind]);
                        break;
                }
                case 'n': {
                        process_name = static_cast<char *>(argv[optind]);
                        break;
                }
                default:
                        std::cout << "Illegal options " << std::endl;
                        return(1);
                }
        }
        std::vector<std::thread> threads;
        if (num_cores) {
                for (i=0; i<num_cores; i++)
                {
                        int ret;
                        cpu_set_t  mask;
                        CPU_ZERO(&mask);
                        CPU_SET(cores[i],&mask);
                        std::thread thr{thread_func};
                        ret = pthread_setaffinity_np(thr.native_handle(), sizeof(cpu_set_t), &mask);
                        if (ret != 0) {
                                std::cerr << "Err::pthread_setaffinity_np(): " << ret << "\n";
                        }
                        threads.push_back(std::move(thr));

                }
        } else {
                threads.push_back(std::thread{thread_func});
                std::this_thread::sleep_for (std::chrono::seconds(1));
        }
        for (auto &t : threads) t.join();
}
